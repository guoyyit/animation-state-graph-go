package runtime

import (
	"fmt"
	"math"
	"math/rand"
	"sort"

	"github.com/guoyyit/animation-state-graph-go/expr"
	"github.com/guoyyit/animation-state-graph-go/graph"
)

// Entity is one animated entity. It holds its own parameters and internal
// state; entities never share mutable state, so they can advance
// concurrently.
type Entity struct {
	id     string
	state  string
	trans  *transState
	params map[string]float64
	has    map[string]bool
	frame  int64
	rng    *rand.Rand
	pose   []float64
}

type transState struct {
	from     string
	to       string
	elapsed  float64
	duration float64
}

// NewEntity creates an entity with the given id. The seed seeds the
// entity-local random source; output is deterministic for a fixed seed and
// parameter sequence.
func NewEntity(id string, seed int64) *Entity {
	return &Entity{
		id:     id,
		params: make(map[string]float64),
		has:    make(map[string]bool),
		rng:    rand.New(rand.NewSource(seed)),
	}
}

// ID returns the entity id.
func (en *Entity) ID() string { return en.id }

// State returns the current state id.
func (en *Entity) State() string { return en.state }

// Frame returns the number of frames advanced.
func (en *Entity) Frame() int64 { return en.frame }

// SetParam sets a float parameter for this entity.
func (en *Entity) SetParam(name string, v float64) {
	en.params[name] = v
	en.has[name] = true
}

// SetBool sets a boolean parameter (true -> 1.0, false -> 0.0).
func (en *Entity) SetBool(name string, v bool) {
	if v {
		en.params[name] = 1
	} else {
		en.params[name] = 0
	}
	en.has[name] = true
}

// SetInt sets an integer parameter.
func (en *Entity) SetInt(name string, v int) {
	en.params[name] = float64(v)
	en.has[name] = true
}

// GetParam implements expr.ParamLookup.
func (en *Entity) GetParam(name string) (float64, bool) {
	v, ok := en.params[name]
	return v, ok
}

// InTransition reports whether the entity is currently crossfading.
func (en *Entity) InTransition() bool { return en.trans != nil }

// TransitionProgress returns the crossfade progress in [0,1] when
// InTransition, otherwise 0.
func (en *Entity) TransitionProgress() float64 {
	if en.trans == nil || en.trans.duration <= 0 {
		return 0
	}
	t := en.trans.elapsed / en.trans.duration
	if t > 1 {
		return 1
	}
	return t
}

// AdvanceResult is the output of one frame for one entity.
type AdvanceResult struct {
	EntityID     string
	Frame        int64
	State        string
	InTransition bool
	Pose         []float64
	Diagnostics  []Diagnostic
}

// Advance advances the entity by dt seconds against the engine's current
// graph snapshot.
//
// The snapshot is loaded once per call, so a hot update happening during the
// call cannot be observed mid-frame. Any unexpected error is recovered and
// reported as a diagnostic instead of panicking.
func (en *Entity) Advance(eng *Engine, dt float64) (res AdvanceResult) {
	defer func() {
		if r := recover(); r != nil {
			res.Diagnostics = append(res.Diagnostics, Diagnostic{
				Kind:     DiagInternal,
				Severity: SevError,
				Binding:  "",
				Message:  fmt.Sprintf("entity %q: internal error: %v", en.id, r),
				Frame:    en.frame,
			})
			if res.Pose == nil {
				res.Pose = en.pose
			}
		}
	}()

	snap := eng.snapshot()
	en.frame++
	res.EntityID = en.id
	res.Frame = en.frame

	diags := make([]Diagnostic, 0, 4)

	if en.state == "" {
		en.state = snap.InitialState()
	}

	en.advanceState(snap, dt, &diags)

	// Compute the output pose.
	n := snap.ChannelCount()
	var pose []float64
	if en.trans != nil {
		from := snap.State(en.trans.from)
		to := snap.State(en.trans.to)
		t := en.TransitionProgress()
		fp := en.evalState(snap, from, &diags)
		tp := en.evalState(snap, to, &diags)
		pose = make([]float64, n)
		for j := 0; j < n; j++ {
			pose[j] = (1-t)*fp[j] + t*tp[j]
		}
		res.InTransition = true
	} else {
		st := snap.State(en.state)
		pose = en.evalState(snap, st, &diags)
	}
	en.pose = pose
	res.Pose = pose
	res.State = en.state

	sort.SliceStable(diags, func(i, j int) bool {
		if diags[i].Binding != diags[j].Binding {
			return diags[i].Binding < diags[j].Binding
		}
		if diags[i].Kind != diags[j].Kind {
			return diags[i].Kind < diags[j].Kind
		}
		return diags[i].Param < diags[j].Param
	})
	res.Diagnostics = diags
	return res
}

// advanceState runs transition adjudication and crossfade progression.
func (en *Entity) advanceState(snap *graph.CompiledGraph, dt float64, diags *[]Diagnostic) {
	if en.trans != nil {
		en.trans.elapsed += dt
		if en.trans.elapsed >= en.trans.duration {
			en.state = en.trans.to
			en.trans = nil
		}
		return
	}

	st := snap.State(en.state)
	if st == nil {
		*diags = append(*diags, Diagnostic{
			Kind: DiagStateNotFound, Severity: SevError,
			Binding: "state." + en.state,
			Message: fmt.Sprintf("entity %q: state %q not in active graph snapshot, falling back to initial state %q",
				en.id, en.state, snap.InitialState()),
			Frame: en.frame,
		})
		en.state = snap.InitialState()
		return
	}

	for _, tr := range st.Transitions() {
		if !en.paramsPresent(tr, diags) {
			continue
		}
		v, ok := tr.Cond(en)
		if !ok {
			continue
		}
		if v == 0 {
			continue
		}
		// A transition fired. At most one transition fires per frame.
		if tr.To == en.state {
			// Self transition: no re-entry, no restart.
			return
		}
		if snap.State(tr.To) == nil {
			*diags = append(*diags, Diagnostic{
				Kind: DiagTargetNotFound, Severity: SevError,
				Binding: "transition." + tr.ID,
				Message: fmt.Sprintf("entity %q: transition %q targets unknown state %q", en.id, tr.ID, tr.To),
				Frame:   en.frame,
			})
			return
		}
		if tr.Duration > 0 {
			en.trans = &transState{from: en.state, to: tr.To, elapsed: 0, duration: tr.Duration}
		} else {
			en.state = tr.To
		}
		return
	}
}

// paramsPresent checks that every parameter referenced by a transition
// condition has been provided by the entity. Missing parameters produce a
// locatable diagnostic; the transition does not fire.
func (en *Entity) paramsPresent(tr *graph.CompiledTransition, diags *[]Diagnostic) bool {
	allPresent := true
	for _, name := range tr.ParamNames {
		if !en.has[name] {
			allPresent = false
			*diags = append(*diags, Diagnostic{
				Kind:     DiagMissingParam,
				Severity: SevWarning,
				Binding:  "transition." + tr.ID + ".condition",
				Param:    name,
				Message:  fmt.Sprintf("entity %q: condition of transition %q references missing parameter %q", en.id, tr.ID, name),
				Degraded: 0,
				Frame:    en.frame,
			})
		}
	}
	return allPresent
}

// evalState computes the blended pose of a state for this frame.
func (en *Entity) evalState(snap *graph.CompiledGraph, st *graph.CompiledState, diags *[]Diagnostic) []float64 {
	n := snap.ChannelCount()
	pose := make([]float64, n)
	if st == nil {
		return pose
	}
	clips := st.Clips()
	weights := make([]float64, len(clips))
	sum := 0.0
	for i, cc := range clips {
		w := 1.0
		for _, b := range cc.Chain {
			w *= en.evalBinding(snap, b, diags)
		}
		weights[i] = w
		sum += w
	}
	if sum <= 0 {
		switch snap.ZeroPolicy() {
		case graph.EqualWeights:
			eq := 1.0 / float64(len(weights))
			for i := range weights {
				weights[i] = eq
			}
		default:
			for i := range weights {
				weights[i] = 0
			}
		}
		*diags = append(*diags, Diagnostic{
			Kind:     DiagDegenerateWeights,
			Severity: SevWarning,
			Binding:  "state." + st.ID(),
			Message:  fmt.Sprintf("entity %q: total blend weight is zero; applying zero-weight policy %q", en.id, snap.ZeroPolicy()),
			Degraded: 0,
			Frame:    en.frame,
		})
	} else {
		for i := range weights {
			weights[i] /= sum
		}
	}
	for i, cc := range clips {
		for j := 0; j < n; j++ {
			pose[j] += weights[i] * cc.Channels[j]
		}
	}
	return pose
}

// evalBinding evaluates one weight binding, applying type/range checks and
// the documented degradation policy.
func (en *Entity) evalBinding(snap *graph.CompiledGraph, b graph.CompiledBinding, diags *[]Diagnostic) float64 {
	pd, _ := snap.ParamDef(b.Param)
	v, present := en.params[b.Param]
	if !present {
		d := b.Def
		*diags = append(*diags, Diagnostic{
			Kind:     DiagMissingParam,
			Severity: SevWarning,
			Binding:  b.Path,
			Param:    b.Param,
			Message:  fmt.Sprintf("entity %q: binding %q references missing parameter %q; using default %g", en.id, b.Path, b.Param, d),
			Degraded: d,
			Frame:    en.frame,
		})
		return d
	}
	if !typeOK(pd.Type, v) {
		d := b.Def
		*diags = append(*diags, Diagnostic{
			Kind:     DiagTypeMismatch,
			Severity: SevWarning,
			Binding:  b.Path,
			Param:    b.Param,
			Value:    v,
			Degraded: d,
			Message:  fmt.Sprintf("entity %q: binding %q parameter %q has type %s but value %g; using default %g",
				en.id, b.Path, b.Param, pd.Type, v, d),
			Frame: en.frame,
		})
		return d
	}
	// Schema range clamp.
	if pd.HasMin && v < pd.Min {
		*diags = append(*diags, Diagnostic{
			Kind: DiagOutOfRange, Severity: SevWarning,
			Binding: b.Path, Param: b.Param, Value: v, Degraded: pd.Min,
			Message: fmt.Sprintf("entity %q: parameter %q=%g below schema min %g; clamped", en.id, b.Param, v, pd.Min),
			Frame:   en.frame,
		})
		v = pd.Min
	}
	if pd.HasMax && v > pd.Max {
		*diags = append(*diags, Diagnostic{
			Kind: DiagOutOfRange, Severity: SevWarning,
			Binding: b.Path, Param: b.Param, Value: v, Degraded: pd.Max,
			Message: fmt.Sprintf("entity %q: parameter %q=%g above schema max %g; clamped", en.id, b.Param, v, pd.Max),
			Frame:   en.frame,
		})
		v = pd.Max
	}
	w := v*b.Scale + b.Offset
	if b.HasMin && w < b.Min {
		*diags = append(*diags, Diagnostic{
			Kind: DiagOutOfRange, Severity: SevWarning,
			Binding: b.Path, Param: b.Param, Value: w, Degraded: b.Min,
			Message: fmt.Sprintf("entity %q: binding %q weight %g below binding min %g; clamped", en.id, b.Path, w, b.Min),
			Frame:   en.frame,
		})
		w = b.Min
	}
	if b.HasMax && w > b.Max {
		*diags = append(*diags, Diagnostic{
			Kind: DiagOutOfRange, Severity: SevWarning,
			Binding: b.Path, Param: b.Param, Value: w, Degraded: b.Max,
			Message: fmt.Sprintf("entity %q: binding %q weight %g above binding max %g; clamped", en.id, b.Path, w, b.Max),
			Frame:   en.frame,
		})
		w = b.Max
	}
	return w
}

func typeOK(t graph.ParamType, v float64) bool {
	switch t {
	case graph.ParamBool:
		return v == 0 || v == 1
	case graph.ParamInt:
		return v == math.Trunc(v)
	default:
		return true
	}
}

// Compile-time assertion that Entity satisfies expr.ParamLookup.
var _ expr.ParamLookup = (*Entity)(nil)
