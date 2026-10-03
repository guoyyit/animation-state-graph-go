package anim

import "fmt"

// Severity 诊断级别。
type Severity int

const (
	SevWarn Severity = iota
	SevError
)

// Diagnostic 描述一次参数绑定问题，可定位到具体绑定。
type Diagnostic struct {
	Severity Severity
	Code     string // "param.missing" / "param.type" / "param.range"
	Binding  string // 绑定位置，如 "blend/walk/clips[0]"
	Param    string
	Message  string
}

func (d Diagnostic) String() string {
	return fmt.Sprintf("[%s] %s binding=%s param=%s: %s", d.Code, sevName(d.Severity), d.Binding, d.Param, d.Message)
}

func sevName(s Severity) string {
	if s == SevError {
		return "error"
	}
	return "warn"
}

// Params 一次推进所使用的外部参数快照。
// 实体之间互不相同；单次 Advance 内只读。
type Params struct {
	floats map[string]float64
	bools  map[string]bool
	ints   map[string]int64
}

// NewParams 创建空参数集。
func NewParams() *Params { return &Params{} }

// SetFloat 设置浮点参数。
func (p *Params) SetFloat(name string, v float64) *Params {
	if p.floats == nil {
		p.floats = map[string]float64{}
	}
	p.floats[name] = v
	return p
}

// SetBool 设置布尔参数。
func (p *Params) SetBool(name string, v bool) *Params {
	if p.bools == nil {
		p.bools = map[string]bool{}
	}
	p.bools[name] = v
	return p
}

// SetInt 设置整型参数。
func (p *Params) SetInt(name string, v int64) *Params {
	if p.ints == nil {
		p.ints = map[string]int64{}
	}
	p.ints[name] = v
	return p
}

// lookupFloat 按期望类型读取一个数值参数。
// 缺失、类型不匹配、越界时返回 ok=false 并写入诊断；
// 越界时同时返回钳制后的值供降级使用。
func (p *Params) lookupFloat(def ParamDef, binding string, diags *[]Diagnostic) (v float64, ok bool) {
	switch def.Type {
	case ParamFloat:
		f, ok := p.floats[def.Name]
		if !ok {
			if _, isBool := p.bools[def.Name]; isBool {
				*diags = append(*diags, Diagnostic{SevError, "param.type", binding, def.Name, "期望 Float，实际为 Bool"})
			} else if _, isInt := p.ints[def.Name]; isInt {
				*diags = append(*diags, Diagnostic{SevError, "param.type", binding, def.Name, "期望 Float，实际为 Int"})
			} else {
				*diags = append(*diags, Diagnostic{SevError, "param.missing", binding, def.Name, "参数缺失"})
			}
			return 0, false
		}
		return p.clampRange(def, f, binding, diags)
	case ParamInt:
		i, ok := p.ints[def.Name]
		if !ok {
			if _, isF := p.floats[def.Name]; isF {
				*diags = append(*diags, Diagnostic{SevError, "param.type", binding, def.Name, "期望 Int，实际为 Float"})
			} else if _, isB := p.bools[def.Name]; isB {
				*diags = append(*diags, Diagnostic{SevError, "param.type", binding, def.Name, "期望 Int，实际为 Bool"})
			} else {
				*diags = append(*diags, Diagnostic{SevError, "param.missing", binding, def.Name, "参数缺失"})
			}
			return 0, false
		}
		return p.clampRange(def, float64(i), binding, diags)
	case ParamBool:
		b, ok := p.bools[def.Name]
		if !ok {
			*diags = append(*diags, Diagnostic{SevError, "param.type", binding, def.Name, "期望 Bool，实际为数值或缺失"})
			return 0, false
		}
		if b {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// clampRange 越界时钳制并记录诊断（降级策略：clamp，不静默放行）。
func (p *Params) clampRange(def ParamDef, v float64, binding string, diags *[]Diagnostic) (float64, bool) {
	// 约定：Min < Max 时范围约束生效；Min >= Max 视为未设置范围。
	if def.Min < def.Max && (v < def.Min || v > def.Max) {
		clamped := v
		if clamped < def.Min {
			clamped = def.Min
		}
		if clamped > def.Max {
			clamped = def.Max
		}
		*diags = append(*diags, Diagnostic{
			Severity: SevWarn, Code: "param.range", Binding: binding, Param: def.Name,
			Message: fmt.Sprintf("取值 %v 超出范围 [%v, %v]，已钳制为 %v", v, def.Min, def.Max, clamped),
		})
		return clamped, true
	}
	return v, true
}
