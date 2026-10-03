package runtime

import (
	"encoding/json"
	"fmt"
)

// TransSnapshot is the serializable form of an in-progress crossfade.
type TransSnapshot struct {
	From     string  `json:"from"`
	To       string  `json:"to"`
	Elapsed  float64 `json:"elapsed"`
	Duration float64 `json:"duration"`
}

// EntitySnapshot is the serializable form of an entity's committed state.
//
// Parameters are external input and are not part of the snapshot; on restart
// the caller re-supplies the same parameter sequence.
type EntitySnapshot struct {
	ID    string          `json:"id"`
	State string          `json:"state"`
	Trans *TransSnapshot  `json:"trans,omitempty"`
	Frame int64           `json:"frame"`
}

// Snapshot captures the entity's committed state for persistence.
func (en *Entity) Snapshot() EntitySnapshot {
	s := EntitySnapshot{
		ID:    en.id,
		State: en.state,
		Frame: en.frame,
	}
	if en.trans != nil {
		s.Trans = &TransSnapshot{
			From:     en.trans.from,
			To:       en.trans.to,
			Elapsed:  en.trans.elapsed,
			Duration: en.trans.duration,
		}
	}
	return s
}

// RestoreEntity rebuilds an entity from a previously committed snapshot.
// Advancing the restored entity with the same parameter and dt sequence
// reproduces the output of an uninterrupted run.
func RestoreEntity(s EntitySnapshot) (*Entity, error) {
	if s.ID == "" {
		return nil, fmt.Errorf("runtime: snapshot has empty entity id")
	}
	en := NewEntity(s.ID, 0)
	en.state = s.State
	en.frame = s.Frame
	if s.Trans != nil {
		if s.Trans.Elapsed < 0 || s.Trans.Duration < 0 {
			return nil, fmt.Errorf("runtime: snapshot for %q has negative transition timing", s.ID)
		}
		en.trans = &transState{
			from:     s.Trans.From,
			to:       s.Trans.To,
			elapsed:  s.Trans.Elapsed,
			duration: s.Trans.Duration,
		}
	}
	return en, nil
}

// MarshalSnapshot serializes a snapshot to JSON.
func MarshalSnapshot(s EntitySnapshot) ([]byte, error) {
	return json.Marshal(s)
}

// UnmarshalSnapshot restores a snapshot from JSON.
func UnmarshalSnapshot(b []byte) (EntitySnapshot, error) {
	var s EntitySnapshot
	if err := json.Unmarshal(b, &s); err != nil {
		return EntitySnapshot{}, err
	}
	return s, nil
}
