package runtime

import "fmt"

// DiagKind identifies the kind of a runtime diagnostic.
type DiagKind int

const (
	// DiagMissingParam: a binding or condition references a parameter the
	// entity has not provided.
	DiagMissingParam DiagKind = iota
	// DiagTypeMismatch: a parameter value does not match its schema type
	// (e.g. a bool parameter given 5.0).
	DiagTypeMismatch
	// DiagOutOfRange: a parameter or binding value exceeded its range and
	// was clamped.
	DiagOutOfRange
	// DiagDegenerateWeights: the total blend weight summed to zero; the
	// configured zero-weight policy was applied.
	DiagDegenerateWeights
	// DiagStateNotFound: the entity's current state does not exist in the
	// active graph snapshot (e.g. after a hot update that removed it).
	DiagStateNotFound
	// DiagTargetNotFound: a transition targeted a state missing from the
	// active snapshot.
	DiagTargetNotFound
	// DiagInternal: an unexpected error was recovered for this entity.
	DiagInternal
)

func (k DiagKind) String() string {
	switch k {
	case DiagMissingParam:
		return "missing_param"
	case DiagTypeMismatch:
		return "type_mismatch"
	case DiagOutOfRange:
		return "out_of_range"
	case DiagDegenerateWeights:
		return "degenerate_weights"
	case DiagStateNotFound:
		return "state_not_found"
	case DiagTargetNotFound:
		return "target_not_found"
	case DiagInternal:
		return "internal"
	default:
		return "unknown"
	}
}

// Severity classifies diagnostics.
type Severity int

const (
	// SevWarning: degraded but well-defined output was produced.
	SevWarning Severity = iota
	// SevError: the entity could not follow the graph as authored.
	SevError
)

func (s Severity) String() string {
	if s == SevError {
		return "error"
	}
	return "warning"
}

// Diagnostic describes one degraded situation encountered during a frame.
//
// Binding locates the offending binding in the graph definition (e.g.
// "state.run.root.children[0].weight") so the source can be found; Param
// names the external parameter involved.
type Diagnostic struct {
	Kind     DiagKind
	Severity Severity
	Binding  string
	Param    string
	Message  string
	Value    float64
	Degraded float64
	Frame    int64
}

func (d Diagnostic) String() string {
	return fmt.Sprintf("[%s/%s] frame=%d binding=%q param=%q value=%g degraded=%g: %s",
		d.Kind, d.Severity, d.Frame, d.Binding, d.Param, d.Value, d.Degraded, d.Message)
}
