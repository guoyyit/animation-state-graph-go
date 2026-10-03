package anim

import (
	"fmt"
	"testing"
)

// BenchmarkAdvanceScalesWithGraphSize 验证单帧推进开销与图总规模无关：
// 状态数从 100 增至 10000，单帧耗时应基本不变（只访问当前状态的出边）。
func BenchmarkAdvanceScalesWithGraphSize(b *testing.B) {
	for _, nStates := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("states=%d", nStates), func(b *testing.B) {
			def := GraphDef{
				Version: "bench",
				Params:  []ParamDef{{Name: "x", Type: ParamFloat}},
				Entry:   "s0",
			}
			def.Blends = append(def.Blends, BlendNodeDef{ID: "shared", Clips: []ClipRef{
				{ClipID: "a", Param: "x"}, {ClipID: "b", ConstWeight: 1},
			}})
			for i := 0; i < nStates; i++ {
				def.States = append(def.States, StateDef{
					ID:      fmt.Sprintf("s%d", i),
					BlendID: "shared",
					Transitions: []TransitionDef{
						{To: fmt.Sprintf("s%d", (i+1)%nStates), Condition: ExprDef{Param: "x", Op: ">", Value: 1e9}},
					},
				})
			}
			rt, err := NewRuntime(def)
			if err != nil {
				b.Fatal(err)
			}
			e := rt.Spawn("e")
			e.SetParams(NewParams().SetFloat("x", 0.5))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				e.Advance(0.016)
			}
		})
	}
}
