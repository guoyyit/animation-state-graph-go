package anim

import "fmt"

// compiledExpr 预编译的条件表达式。
// 参数缺失或类型不匹配时求值为 false（不转移），并记录诊断。
type compiledExpr struct {
	def      ExprDef
	paramDef ParamDef // 编译期解析的参数定义
}

func compileExpr(def ExprDef, params map[string]ParamDef) (*compiledExpr, error) {
	pd, ok := params[def.Param]
	if !ok {
		return nil, fmt.Errorf("表达式引用了未定义的参数 %q", def.Param)
	}
	switch def.Op {
	case ">", ">=", "<", "<=", "==", "!=":
	default:
		return nil, fmt.Errorf("不支持的比较运算符 %q", def.Op)
	}
	return &compiledExpr{def: def, paramDef: pd}, nil
}

// eval 对参数求值；参数异常时返回 false 并记录诊断。
func (e *compiledExpr) eval(p *Params, binding string, diags *[]Diagnostic) bool {
	v, ok := p.lookupFloat(e.paramDef, binding, diags)
	if !ok {
		return false
	}
	t := e.def.Value
	switch e.def.Op {
	case ">":
		return v > t
	case ">=":
		return v >= t
	case "<":
		return v < t
	case "<=":
		return v <= t
	case "==":
		return v == t
	case "!=":
		return v != t
	}
	return false
}
