// Package anim 提供动画状态图运行时：按外部参数在状态间切换，
// 并对多个动画片段做加权混合。
package anim

// ParamType 参数类型。
type ParamType int

const (
	ParamFloat ParamType = iota
	ParamBool
	ParamInt
)

// ParamDef 描述一个外部参数的名称、类型与取值范围。
type ParamDef struct {
	Name string
	Type ParamType
	Min  float64 // 仅 Float/Int 有效
	Max  float64 // 仅 Float/Int 有效
}

// ClipRef 引用一个动画片段及其播放权重来源。
type ClipRef struct {
	ClipID      string  // 动画片段标识
	Param       string  // 权重来源参数（Float），为空则使用 ConstWeight
	ConstWeight float64 // 固定权重
}

// BlendNodeDef 混合节点：对若干片段按权重混合，权重归一化。
type BlendNodeDef struct {
	ID    string
	Clips []ClipRef
}

// StateDef 状态：绑定一个混合节点，并声明出边转移。
type StateDef struct {
	ID          string
	BlendID     string
	Transitions []TransitionDef
}

// TransitionDef 状态转移：条件满足时从源状态切换到目标状态。
type TransitionDef struct {
	To        string
	Condition ExprDef // 为空表示无条件（每帧成立）
	Priority  int     // 同帧多转移满足时，数值小者优先
	Duration  float64 // 过渡时长（秒），0 表示瞬时
}

// ExprDef 参数条件表达式（单参数比较）。
type ExprDef struct {
	Param string
	Op    string // ">", ">=", "<", "<=", "==", "!="
	Value float64
}

// GraphDef 状态图定义（用户提交的未编译形式）。
type GraphDef struct {
	Version string
	Params  []ParamDef
	Blends  []BlendNodeDef
	States  []StateDef
	Entry   string // 入口状态 ID
}
