package runtime

import (
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/guoyyit/animation-state-graph-go/graph"
)

// Engine holds the current graph snapshot and advances entities against it.
//
// The snapshot is stored in an atomic pointer: hot update compiles a brand
// new CompiledGraph and swaps the pointer atomically, so an entity advancing
// during an update never sees a half-updated graph. A failed validation
// leaves the previous snapshot in place.
type Engine struct {
	snap atomic.Pointer[graph.CompiledGraph]
	seed int64
}

// New compiles def and returns an engine. On validation failure the error is
// returned and no engine is created.
func New(def *graph.GraphDef, seed int64) (*Engine, error) {
	g, err := graph.Compile(def)
	if err != nil {
		return nil, err
	}
	e := &Engine{seed: seed}
	e.snap.Store(g)
	return e, nil
}

// Update compiles def and, on success, atomically replaces the active
// snapshot. On validation failure the previous snapshot stays active and the
// error is returned.
func (e *Engine) Update(def *graph.GraphDef) error {
	g, err := graph.Compile(def)
	if err != nil {
		return err
	}
	e.snap.Store(g)
	return nil
}

// Snapshot returns the active compiled graph. The returned pointer is
// immutable and safe for concurrent reads.
func (e *Engine) Snapshot() *graph.CompiledGraph { return e.snapshot() }

func (e *Engine) snapshot() *graph.CompiledGraph { return e.snap.Load() }

// Seed returns the engine's base random seed.
func (e *Engine) Seed() int64 { return e.seed }

// AdvanceAll advances every entity by dt using a worker pool.
//
// Results are returned in the same order as the input entities, so output is
// independent of scheduling. Each entity runs in its own job; a slow entity
// does not block others, and a panic in one entity is recovered into that
// entity's diagnostics.
//
// workers <= 0 means runtime.NumCPU.
func (e *Engine) AdvanceAll(dt float64, entities []*Entity, workers int) []AdvanceResult {
	results := make([]AdvanceResult, len(entities))
	if len(entities) == 0 {
		return results
	}
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	if workers > len(entities) {
		workers = len(entities)
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				results[i] = entities[i].Advance(e, dt)
			}
		}()
	}
	for i := range entities {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return results
}
