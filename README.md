# animation-state-graph-go

一套动画状态图运行时：在多个实体之间按外部参数在状态之间切换，并对多个
动画片段做加权混合。同一进程内服务大量实体，图定义可运行期热更新；状态
推进并发安全、结果可复现。

## 架构

```
expr      条件表达式解析与预编译（AST -> 不可变闭包）
graph     图定义（GraphDef）、校验、预编译快照（CompiledGraph）
runtime   引擎（Engine）、实体（Entity）、并发推进、诊断、状态序列化
```

- `graph.Compile` 校验图定义并产出**不可变**的 `CompiledGraph`。
- `runtime.Engine` 通过 `atomic.Pointer` 持有当前快照；热更新时编译新快照
  并原子替换，推进中的实体永远读到完整快照。
- 每个 `runtime.Entity` 持有各自的参数与内部状态，实体间无共享可变数据。

## 状态机语义

- 每个实体任意时刻处于一个状态；每帧最多裁决并触发**一条**转移。
- 转移条件为参数表达式（见下）。同一帧多条转移满足时：
  1. 优先级 `Priority` 高的胜出；
  2. 优先级相同按转移 ID 字典序升序裁决。
  该顺序在编译期排定，与遍历顺序无关，结果确定。
- `From: "*"` 为通配转移，匹配任意状态；通配转移与状态自有转移合并排序。
- **自我转移**（目标 == 当前状态）不触发进入动作、不重启状态、不开始
  crossfade，直接跳过。
- **环状转移**不会在一帧内重复进入：每帧只触发一条转移，状态推进按帧
  离散进行（A->B 与 B->A 同时满足时，本帧走一条，下一帧才可能走另一条）。
- 转移时长 `Duration > 0` 时进入 crossfade：期间输出为出状态与入状态姿势的
  加权混合（见下）；`Duration == 0` 为当帧直接切换。
- crossfade 期间不再裁决新转移；crossfade 结束当帧切换到目标状态，下一帧
  起才允许从目标状态转出。

### crossfade 的连续性

crossfade 进度 `t = elapsed / duration`，输出

```
pose = (1-t) * blend(fromState) + t * blend(toState)
```

- 转移触发帧 `t=0`，输出与触发前完全一致；
- 结束帧 `t=1`，输出为目标状态姿势。

因此任意中间时刻的输出都是可解释的中间状态，权重与输出均无跳变。

## 混合规则

- 每个状态有一棵混合树：叶子是动画片段（固定通道向量），每个叶子带一条
  权重绑定；嵌套节点的权重为根到叶子各绑定权重的乘积。
- 绑定权重：`w = Clamp(param * Scale + Offset, Min, Max)`。
- 同一混合节点内对权重做**归一化**：`w_i / Σw`；嵌套节点逐层归一化。
- 权重和为 0（退化权重）时的策略由 `GraphDef.ZeroWeight` 决定：
  - `ZeroPose`：输出全零姿势（默认）；
  - `EqualWeights`：所有片段等权混合。
  两种策略都会产生一条 `degenerate_weights` 诊断。

## 参数配置

参数在 `GraphDef.Params` 中声明：

| 字段 | 说明 |
| --- | --- |
| `Name` | 参数名（绑定与条件表达式通过名字引用） |
| `Type` | `float` / `int` / `bool` |
| `Default` | 缺失或非法时的降级默认值 |
| `HasMin/Min`、`HasMax/Max` | 取值范围（含边界） |

权重绑定 `BindingDef`：

| 字段 | 说明 |
| --- | --- |
| `Param` | 引用的参数名（必须在 schema 中声明，否则编译失败） |
| `Scale` / `Offset` | 权重 = param*scale + offset（scale/offset 均为 0 时 scale 取 1） |
| `Default` | 可选的绑定级默认值；缺省回退到参数 schema 默认值 |
| `HasMin/Min`、`HasMax/Max` | 绑定级权重范围，超出钳制 |

### 异常降级策略（不静默放行）

| 诊断 | 触发条件 | 降级行为 |
| --- | --- | --- |
| `missing_param` | 实体未提供该参数 | 使用绑定默认值（条件参数缺失则该转移不触发） |
| `type_mismatch` | 参数值与 schema 类型不符（bool 给了非 0/1，int 给了小数） | 使用绑定默认值 |
| `out_of_range` | 参数值或绑定权重超出范围 | 钳制到边界 |
| `degenerate_weights` | 混合权重和为 0 | 按 `ZeroWeight` 策略输出 |
| `state_not_found` | 热更新后实体当前状态不存在 | 回退到初始状态 |
| `target_not_found` | 转移目标状态不存在 | 取消该转移 |
| `internal` | 推进中出现未预期错误（panic） | 恢复并报告，不拖垮其他实体 |

每条诊断都带 `Binding` 定位串（如 `state.idle.root.children[0].weight`）、
`Param`、帧号与降级后的值，可区分、可定位到具体绑定。

## 条件表达式

转移条件为表达式字符串，支持：

- 比较：`== != < > <= >=`
- 逻辑：`&& || !`
- 算术：`+ - * /`、括号、一元负号
- 标识符为参数名；字面量为浮点数。

示例：`speed > 1.5 && grounded == 1`。
表达式在编译期解析为不可变闭包，并校验引用的参数均存在于 schema。

## 热更新

1. `engine.Update(newDef)` 先 `graph.Compile` 校验；
2. 校验失败：返回错误，**旧快照继续生效**，实体无感知；
3. 校验成功：原子指针替换为新快照。

推进中的实体在整帧内使用同一份快照（`Advance` 开始时加载一次），不会读到
半更新状态。若新图移除了实体当前所在状态，实体会回退到新图初始状态并
产出 `state_not_found` 诊断；进行中的 crossfade 引用的状态被移除时同样按
缺失处理，不会 panic。

## 并发模型

- `Engine.AdvanceAll(dt, entities, workers)` 用 worker 池推进，结果按实体
  输入顺序返回。
- 每个实体必须只被一个 worker 推进；实体间无共享可变状态。
- 慢实体只占用自己的 job，不阻塞其他实体。
- 单个实体 panic 被 `recover` 捕获并转为该实体的 `internal` 诊断。

## 确定性与可复现

- 裁决顺序、诊断排序均在编译期/帧内排定，不受 map 遍历或调度顺序影响。
- 每个实体持有独立的 `math/rand`（`NewEntity(id, seed)`）；固定种子与相同
  参数序列下，多次运行、不同并发度（workers=1/2/4/8/16）输出逐条一致。
- 实体的已提交状态（当前状态、crossfade 进度、帧号）可通过
  `Entity.Snapshot` / `RestoreEntity` 序列化恢复；恢复后以相同参数序列继续
  推进，输出与不中断运行一致。参数是外部输入，不进入快照。

## 单次推进开销上界

单帧开销为 **O(k + c)**：

- `k` = 当前状态的出转移条数（条件求值，编译期已排好序）；
- `c` = 当前状态混合树的叶子片段数（权重链长度为嵌套深度）。

不随总图状态数增长：编译期已按状态索引转移表、并把混合树拍平为叶子列表，
运行期无需扫描整张图。参考数据（200 状态、每状态 2 片段 2 转移）：

- 单实体单帧约 ~0.9 µs；
- 1000 实体（8 workers）单帧约 ~1.5 ms。

## 用法示例

```go
def := &graph.GraphDef{ /* 参数、片段、状态、转移 */ }
eng, err := runtime.New(def, 42)
if err != nil { /* 图定义非法 */ }

en := runtime.NewEntity("player", 1)
en.SetParam("speed", 3.0)
en.SetBool("grounded", true)

res := en.Advance(eng, 1.0/60.0)
// res.Pose          当前姿势
// res.State         当前状态
// res.InTransition  是否处于 crossfade
// res.Diagnostics   本帧诊断（缺失/越界/……）

// 并发推进
results := eng.AdvanceAll(1.0/60.0, entities, 8)

// 热更新
if err := eng.Update(newDef); err != nil {
    // 旧图继续运行
}

// 持久化与恢复
snap := en.Snapshot()
data, _ := runtime.MarshalSnapshot(snap)
restored, _ := runtime.UnmarshalSnapshot(data)
en2, _ := runtime.RestoreEntity(restored)
```
