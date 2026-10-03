// Package graph defines the animation state graph: states, blend trees,
// clips, transitions and parameter schemas.
//
// A GraphDef is the authoring format. It is validated and compiled into an
// immutable CompiledGraph by Compile. The compiled form is the only thing the
// runtime touches during frame advancement, so a running entity never
// observes a half-updated graph: hot update swaps in a brand new compiled
// snapshot atomically.
package graph

import (
	"fmt"
	"sort"

	"github.com/guoyyit/animation-state-graph-go/expr"
)

// ParamType identifies the value type of an external parameter.
type ParamType int

const (
	// ParamFloat is a floating point parameter.
	ParamFloat ParamType = iota
	// ParamInt is an integer parameter.
	ParamInt
	// ParamBool is a boolean parameter (1.0 / 0.0).
	ParamBool
)

func (t ParamType) String() string {
	switch t {
	case ParamInt:
		return "int"
	case ParamBool:
		return "bool"
	default:
		return "float"
	}
}

// ParamDef describes one external parameter.
type ParamDef struct {
	Name    string
	Type    ParamType
	Default float64
	HasMin  bool
	HasMax  bool
	Min     float64
	Max     float64
}

// ClipDef is an animation clip. A clip pose is a fixed vector of channel
// values; blended poses are weighted sums of clip poses.
type ClipDef struct {
	ID       string
	Channels []float64
}

// BindingDef binds one blend weight to an external parameter.
//
// The evaluated weight is Clamp(Param*Scale+Offset, Min, Max). When the
// parameter is missing or invalid the binding falls back to Default (if set)
// or to the parameter schema default.
type BindingDef struct {
	Param   string
	Scale   float64 // default 1
	Offset  float64 // default 0
	Default *float64
	HasMin  bool
	HasMax  bool
	Min     float64
	Max     float64
}

// BlendChildDef is one child of a blend node: either a clip or a nested
// blend node, with a weight binding.
type BlendChildDef struct {
	Clip   *string
	Node   *BlendNodeDef
	Weight BindingDef
}

// BlendNodeDef blends its children. Children weights are normalized within
// the node; nested node weights multiply through.
type BlendNodeDef struct {
	ID       string
	Children []BlendChildDef
}

// StateDef is one state of the graph.
type StateDef struct {
	ID   string
	Root BlendNodeDef
}

// TransitionDef is a directed edge between states.
//
// From may be "*" to match any state. When several transitions of the same
// state fire in one frame they are adjudicated by Priority (higher wins);
// ties are broken by ID (lexicographic ascending). At most one transition
// fires per frame.
type TransitionDef struct {
	ID        string
	From      string
	To        string
	Condition string
	Duration  float64 // crossfade duration in seconds; 0 = instant
	Priority  int
}

// ZeroWeightPolicy controls what a state does when the total blend weight
// sums to zero.
type ZeroWeightPolicy int

const (
	// ZeroPose outputs an all-zero pose and emits a DegenerateWeights
	// diagnostic.
	ZeroPose ZeroWeightPolicy = iota
	// EqualWeights falls back to equal weights across all clips and emits a
	// DegenerateWeights diagnostic.
	EqualWeights
)

// GraphDef is the authoring format of an animation state graph.
type GraphDef struct {
	Name         string
	Params       []ParamDef
	Clips        []ClipDef
	States       []StateDef
	Transitions  []TransitionDef
	InitialState string
	ZeroWeight   ZeroWeightPolicy
}

// CompiledGraph is an immutable, validated, precompiled snapshot of a graph.
type CompiledGraph struct {
	def          *GraphDef
	params       map[string]ParamDef
	clips        map[string]*ClipDef
	states       map[string]*CompiledState
	stateOrder   []string
	initial      string
	channelCount int
	zeroPolicy   ZeroWeightPolicy
}

// CompiledState is the precompiled form of a state: a flattened list of
// weighted clips and a sorted list of outgoing transitions.
type CompiledState struct {
	id          string
	clips       []CompiledClip
	transitions []*CompiledTransition
}

// ID returns the state id.
func (s *CompiledState) ID() string { return s.id }

// Clips returns the flattened, precompiled blend tree of the state.
func (s *CompiledState) Clips() []CompiledClip { return s.clips }

// Transitions returns the outgoing transitions, sorted by priority
// (descending) then id (ascending).
func (s *CompiledState) Transitions() []*CompiledTransition { return s.transitions }

// CompiledClip is one leaf of a state's blend tree: a clip with the chain of
// weight bindings from the root blend node down to the leaf.
type CompiledClip struct {
	ClipID   string
	Channels []float64
	Chain    []CompiledBinding
}

// CompiledBinding is the precompiled form of a weight binding.
type CompiledBinding struct {
	Param   string
	Scale   float64
	Offset  float64
	HasDef  bool
	Def     float64
	HasMin  bool
	HasMax  bool
	Min     float64
	Max     float64
	Path    string
}

// CompiledTransition is the precompiled form of a transition.
type CompiledTransition struct {
	ID         string
	To         string
	Duration   float64
	Priority   int
	Cond       func(expr.ParamLookup) (float64, bool)
	ParamNames []string
	CondSrc    string
}

// Def returns the original definition this snapshot was compiled from.
func (g *CompiledGraph) Def() *GraphDef { return g.def }

// ParamDef returns the schema of a parameter.
func (g *CompiledGraph) ParamDef(name string) (ParamDef, bool) {
	p, ok := g.params[name]
	return p, ok
}

// State returns the compiled state with the given id, or nil.
func (g *CompiledGraph) State(id string) *CompiledState { return g.states[id] }

// StateIDs returns all state ids in deterministic (lexicographic) order.
func (g *CompiledGraph) StateIDs() []string {
	out := make([]string, len(g.stateOrder))
	copy(out, g.stateOrder)
	return out
}

// ChannelCount returns the number of pose channels.
func (g *CompiledGraph) ChannelCount() int { return g.channelCount }

// InitialState returns the state entities start in.
func (g *CompiledGraph) InitialState() string { return g.initial }

// ZeroPolicy returns the configured zero-weight policy.
func (g *CompiledGraph) ZeroPolicy() ZeroWeightPolicy { return g.zeroPolicy }

// Compile validates a graph definition and produces an immutable compiled
// snapshot. The returned error describes the first validation failure; the
// caller keeps using its previous snapshot in that case.
func Compile(def *GraphDef) (*CompiledGraph, error) {
	if def == nil {
		return nil, fmt.Errorf("graph: nil definition")
	}
	g := &CompiledGraph{
		def:    def,
		params: make(map[string]ParamDef, len(def.Params)),
		clips:  make(map[string]*ClipDef, len(def.Clips)),
		states: make(map[string]*CompiledState, len(def.States)),
	}

	// Params.
	for i := range def.Params {
		p := def.Params[i]
		if p.Name == "" {
			return nil, fmt.Errorf("graph: param #%d has empty name", i)
		}
		if _, exists := g.params[p.Name]; exists {
			return nil, fmt.Errorf("graph: duplicate param %q", p.Name)
		}
		if p.Type != ParamFloat && p.Type != ParamInt && p.Type != ParamBool {
			return nil, fmt.Errorf("graph: param %q has invalid type %d", p.Name, p.Type)
		}
		if p.HasMin && p.HasMax && p.Min > p.Max {
			return nil, fmt.Errorf("graph: param %q min > max", p.Name)
		}
		g.params[p.Name] = p
	}

	// Clips.
	channelCount := -1
	for i := range def.Clips {
		c := def.Clips[i]
		if c.ID == "" {
			return nil, fmt.Errorf("graph: clip #%d has empty id", i)
		}
		if _, exists := g.clips[c.ID]; exists {
			return nil, fmt.Errorf("graph: duplicate clip %q", c.ID)
		}
		if channelCount == -1 {
			channelCount = len(c.Channels)
		} else if len(c.Channels) != channelCount {
			return nil, fmt.Errorf("graph: clip %q has %d channels, want %d", c.ID, len(c.Channels), channelCount)
		}
		g.clips[c.ID] = &def.Clips[i]
	}
	if channelCount < 0 {
		channelCount = 0
	}
	g.channelCount = channelCount

	// States + blend trees.
	stateIDs := make(map[string]bool, len(def.States))
	for i := range def.States {
		s := def.States[i]
		if s.ID == "" {
			return nil, fmt.Errorf("graph: state #%d has empty id", i)
		}
		if stateIDs[s.ID] {
			return nil, fmt.Errorf("graph: duplicate state %q", s.ID)
		}
		stateIDs[s.ID] = true
		cs := &CompiledState{id: s.ID}
		if err := compileBlend(&s.Root, g, cs, "root", nil); err != nil {
			return nil, err
		}
		g.states[s.ID] = cs
		g.stateOrder = append(g.stateOrder, s.ID)
	}
	if len(def.States) == 0 {
		return nil, fmt.Errorf("graph: no states defined")
	}
	sort.Strings(g.stateOrder)

	// Initial state.
	initial := def.InitialState
	if initial == "" {
		initial = g.stateOrder[0]
	}
	if _, ok := g.states[initial]; !ok {
		return nil, fmt.Errorf("graph: initial state %q not found", initial)
	}
	g.initial = initial

	// Transitions.
	transByState := make(map[string][]*CompiledTransition)
	transIDs := make(map[string]bool)
	for i := range def.Transitions {
		t := def.Transitions[i]
		if t.ID == "" {
			return nil, fmt.Errorf("graph: transition #%d has empty id", i)
		}
		if transIDs[t.ID] {
			return nil, fmt.Errorf("graph: duplicate transition %q", t.ID)
		}
		transIDs[t.ID] = true
		if t.From != "*" {
			if _, ok := g.states[t.From]; !ok {
				return nil, fmt.Errorf("graph: transition %q from unknown state %q", t.ID, t.From)
			}
		}
		if _, ok := g.states[t.To]; !ok {
			return nil, fmt.Errorf("graph: transition %q to unknown state %q", t.ID, t.To)
		}
		if t.Duration < 0 {
			return nil, fmt.Errorf("graph: transition %q has negative duration", t.ID)
		}
		if t.Condition == "" {
			return nil, fmt.Errorf("graph: transition %q has empty condition", t.ID)
		}
		node, err := expr.Parse(t.Condition)
		if err != nil {
			return nil, fmt.Errorf("graph: transition %q condition: %w", t.ID, err)
		}
		cond, err := expr.Compile(node)
		if err != nil {
			return nil, fmt.Errorf("graph: transition %q condition: %w", t.ID, err)
		}
		names, err := checkConditionParams(node, g.params, t.ID)
		if err != nil {
			return nil, err
		}
		ct := &CompiledTransition{
			ID:         t.ID,
			To:         t.To,
			Duration:   t.Duration,
			Priority:   t.Priority,
			Cond:       cond,
			ParamNames: names,
			CondSrc:    t.Condition,
		}
		from := t.From
		if from == "" {
			from = "*"
		}
		transByState[from] = append(transByState[from], ct)
	}
	for id, cs := range g.states {
		list := append(append([]*CompiledTransition{}, transByState[id]...), transByState["*"]...)
		sort.SliceStable(list, func(i, j int) bool {
			if list[i].Priority != list[j].Priority {
				return list[i].Priority > list[j].Priority
			}
			return list[i].ID < list[j].ID
		})
		cs.transitions = list
	}

	g.zeroPolicy = def.ZeroWeight
	return g, nil
}

// checkConditionParams walks the condition AST, verifies every referenced
// parameter exists in the schema and returns the referenced names.
func checkConditionParams(n expr.Node, params map[string]ParamDef, transID string) ([]string, error) {
	seen := make(map[string]bool)
	var names []string
	var walk func(n expr.Node) error
	walk = func(n expr.Node) error {
		switch t := n.(type) {
		case *expr.ParamRef:
			if _, ok := params[t.Name]; !ok {
				return fmt.Errorf("graph: transition %q references unknown parameter %q", transID, t.Name)
			}
			if !seen[t.Name] {
				seen[t.Name] = true
				names = append(names, t.Name)
			}
		case *expr.Unary:
			return walk(t.Operand)
		case *expr.Binary:
			if err := walk(t.Left); err != nil {
				return err
			}
			return walk(t.Right)
		}
		return nil
	}
	if err := walk(n); err != nil {
		return nil, err
	}
	sort.Strings(names)
	return names, nil
}

// compileBlend recursively flattens a blend tree into per-clip weight
// chains. chain holds the weight bindings from the root to the current node.
func compileBlend(node *BlendNodeDef, g *CompiledGraph, cs *CompiledState, path string, chain []CompiledBinding) error {
	if node.ID == "" {
		return fmt.Errorf("graph: blend node in %s has empty id", path)
	}
	if len(node.Children) == 0 {
		return fmt.Errorf("graph: blend node %q has no children", path)
	}
	for i := range node.Children {
		ch := node.Children[i]
		childPath := fmt.Sprintf("%s.children[%d]", path, i)
		b, err := compileBinding(ch.Weight, g, childPath+".weight")
		if err != nil {
			return err
		}
		childChain := append(append([]CompiledBinding{}, chain...), b)
		switch {
		case ch.Clip != nil && ch.Node != nil:
			return fmt.Errorf("graph: %s has both clip and node", childPath)
		case ch.Clip != nil:
			clip, ok := g.clips[*ch.Clip]
			if !ok {
				return fmt.Errorf("graph: %s references unknown clip %q", childPath, *ch.Clip)
			}
			cs.clips = append(cs.clips, CompiledClip{
				ClipID:   clip.ID,
				Channels: append([]float64(nil), clip.Channels...),
				Chain:    childChain,
			})
		case ch.Node != nil:
			if err := compileBlend(ch.Node, g, cs, childPath+".node", childChain); err != nil {
				return err
			}
		default:
			return fmt.Errorf("graph: %s has neither clip nor node", childPath)
		}
	}
	return nil
}

func compileBinding(b BindingDef, g *CompiledGraph, path string) (CompiledBinding, error) {
	if b.Param == "" {
		return CompiledBinding{}, fmt.Errorf("graph: binding at %s has empty param", path)
	}
	pd, ok := g.params[b.Param]
	if !ok {
		return CompiledBinding{}, fmt.Errorf("graph: binding at %s references unknown parameter %q", path, b.Param)
	}
	cb := CompiledBinding{
		Param:  b.Param,
		Scale:  b.Scale,
		Offset: b.Offset,
		HasMin: b.HasMin,
		HasMax: b.HasMax,
		Min:    b.Min,
		Max:    b.Max,
		Path:   path,
	}
	if b.Scale == 0 && b.Offset == 0 {
		cb.Scale = 1
	}
	if b.HasMin && b.HasMax && b.Min > b.Max {
		return CompiledBinding{}, fmt.Errorf("graph: binding at %s min > max", path)
	}
	if b.Default != nil {
		cb.HasDef = true
		cb.Def = *b.Default
	} else {
		cb.HasDef = true
		cb.Def = pd.Default
	}
	return cb, nil
}
