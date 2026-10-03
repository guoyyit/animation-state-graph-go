package anim

import "fmt"

// ClipSample 一个片段的采样权重（混合输出单元）。
// Weight 为归一化后的权重，一帧输出的所有 Weight 之和为 1。
type ClipSample struct {
	ClipID string
	Weight float64
}

// compiledBlend 预编译的混合节点。
// 权重规则：
//   - ClipRef.Param 非空时权重来自该 Float 参数；否则使用 ConstWeight。
//   - 参数缺失/类型不匹配：该片段权重降级为 0 并记录诊断（不静默放行）。
//   - 负权重钳制为 0。
//   - 所有有效权重按总和归一化；总和为 0 时回退为第一个片段权重 1（确定性兜底）。
type compiledBlend struct {
	id    string
	clips []ClipRef
	// 每个 clip 对应的参数定义（Param 为空时为 nil）
	paramDefs []*ParamDef
}

func compileBlend(def BlendNodeDef, params map[string]ParamDef) (*compiledBlend, error) {
	if def.ID == "" {
		return nil, fmt.Errorf("混合节点 ID 不能为空")
	}
	if len(def.Clips) == 0 {
		return nil, fmt.Errorf("混合节点 %q 至少需要一个片段", def.ID)
	}
	b := &compiledBlend{id: def.ID, clips: def.Clips}
	for i, c := range def.Clips {
		if c.ClipID == "" {
			return nil, fmt.Errorf("混合节点 %q 的第 %d 个片段 ClipID 为空", def.ID, i)
		}
		if c.Param != "" {
			pd, ok := params[c.Param]
			if !ok {
				return nil, fmt.Errorf("混合节点 %q 片段 %q 引用了未定义的参数 %q", def.ID, c.ClipID, c.Param)
			}
			if pd.Type != ParamFloat {
				return nil, fmt.Errorf("混合节点 %q 片段 %q 的权重参数 %q 必须是 Float", def.ID, c.ClipID, c.Param)
			}
			p := pd
			b.paramDefs = append(b.paramDefs, &p)
		} else {
			b.paramDefs = append(b.paramDefs, nil)
		}
	}
	return b, nil
}

// eval 计算归一化权重，输出顺序与定义顺序一致（确定性）。
func (b *compiledBlend) eval(p *Params, diags *[]Diagnostic) []ClipSample {
	raw := make([]float64, len(b.clips))
	total := 0.0
	for i, c := range b.clips {
		w := c.ConstWeight
		if pd := b.paramDefs[i]; pd != nil {
			binding := fmt.Sprintf("blend/%s/clips[%d]", b.id, i)
			v, ok := p.lookupFloat(*pd, binding, diags)
			if !ok {
				w = 0 // 降级：该片段退出混合
			} else {
				w = v
			}
		}
		if w < 0 {
			w = 0
		}
		raw[i] = w
		total += w
	}
	out := make([]ClipSample, len(b.clips))
	if total <= 0 {
		// 确定性兜底：第一个片段独占权重，避免输出空洞。
		for i, c := range b.clips {
			out[i] = ClipSample{ClipID: c.ClipID}
		}
		out[0].Weight = 1
		return out
	}
	for i, c := range b.clips {
		out[i] = ClipSample{ClipID: c.ClipID, Weight: raw[i] / total}
	}
	return out
}
