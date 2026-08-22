# 正式交付与变更单 — 设计

## 版本历史
| 版本 | 日期 | 变更 |
| --- | --- | --- |
| v1 | 2026-08-20 | 初始设计 |
| v2 | 2026-08-22 | 交付 T-301 冻结范围、V1–V3 状态机与正式执行 Outbox |
| v3 | 2026-08-22 | 交付 T-302 权威反馈、链上 work nonce 门控、差异与签名证明 |
| v4 | 2026-08-22 | 交付 T-303 变更单生命周期、责任资金策略与 V4/V5 授权 |

## 架构与影响层
Engine 交付聚合拥有版本分配、反馈与责任；合约拥有 work nonce 和资金资格；Worker 执行不可变命令；BFF/Web 渲染时间线、差异和链上状态。

## 组件/模块边界
拆分范围快照、版本分配、执行尝试、反馈、证明、验收、变更单与责任策略模块。

## API 或接口契约
命令绑定 assignment、交付单元、套餐、范围修订/哈希、父版本/哈希、版本、聚合版本、work nonce、策略、截止时间、成本上限和幂等键。

## 数据模型与迁移
追加正式套餐、范围快照、版本、尝试、反馈集/条目、证明、验收意图、变更单、责任决定和迁移事件；强制套餐/版本唯一及单活动执行。

## 状态、失败、重试与并发
版本分配与 Outbox 原子执行；网络尝试复用逻辑执行/版本；迟到回调仅保留审计；变更单必须已接受并确认所需资金后才能生效。

## 安全与隐私
每个操作都校验角色/所有权/状态，交付物访问需授权，内容哈希与签名证明防止替换。Agent 责任永不得从 Agent 资产创建借记。

## 可观测性与运维
跟踪生成、重试、过时回调、评审时长、范围分类、变更单注资与验收确认延迟。

## 发布、兼容与回滚
证明与责任策略必须版本化。可加性模式使回滚期间仍可只读历史。

## 技术决策与备选方案
Engine 版本号是链下权威，合约 work nonce 约束资金资格。范围变更创建新快照，不覆盖旧快照。

## T-301 冻结范围与正式版本协议

### 权威边界

- `delivery` 聚合只接受当前链上投影的 `active_assignment`，并在创建标准套餐时一次性冻结范围。
- `formal-scope-v1` 绑定任务规格哈希、选中概览及内容哈希、输入快照、验收标准、格式/数量/语言、允许工具、有效外部成本上限和排除项。范围行、请求、事件和计费结果均禁止更新或删除。
- 标准套餐固定包含 V1–V3、最大版本数为 5；T-301 只允许分配包含版本，V4/V5 留给已生效变更单，不能从此入口绕过。
- 每个正式版本只有一个稳定 `logical_execution_id`。数据库事务同时分配版本、推进聚合版本并写入 `agent.execution.formal.requested` Outbox，因此网络重投复用同一逻辑执行和版本。

### 状态与并发

`allocated -> generating -> review | failed`。`review` 和 `failed` 为不可变终态；套餐上的部分唯一索引保证任意时刻最多一个 `allocated/generating` 版本。任务级事务 advisory lock、请求幂等记录、套餐/版本唯一键和计费唯一键共同防止并发启动、重复回调和重复计费。

V2/V3 的领域状态机要求上一版本处于 `review`、父版本及内容哈希一致、反馈集合 ID/摘要/聚合版本完整且 work nonce 严格递增；权威反馈与 canonical 链上 nonce 校验器在 Service 与事务内双重执行。

### API

- `POST /v1/tasks/{taskId}/formal-packages/start`：Publisher、任务所有权和 `Idempotency-Key` 必须同时有效；V1 请求使用 `expectedPackageVersion=0, workNonce=1`。
- `GET /v1/tasks/{taskId}/formal-package`：返回一个权威快照中的套餐、冻结范围和版本时间线。
- BFF 仅代理严格任务绑定的 `/api/tasks/{taskId}/formal-package` 与 `/start` 路径，浏览器不接触 Engine 地址或内部 worker 状态写接口。

## T-302 权威反馈、差异与证明

- Publisher 通过 `POST /v1/tasks/{taskId}/formal-feedback` 提交带幂等键的结构化反馈。每项绑定验收标准、分类、优先级、目标、期望结果和范围声明；反馈集及条目均为不可变追加记录。提交事务锁定任务与套餐，核验当前评审版本和内容哈希，并推进套餐聚合版本。
- 包含版本 V2/V3 只接受全部标记为 `in_scope` 的反馈集。Service 预检后，Repository 在版本分配事务内再次核验反馈 ID/摘要、父版本/哈希、反馈聚合版本及套餐当前聚合，防止检查与写入之间的竞态。
- `WorkNonceAdvanced` 投影会把解码后的 nonce 写入结构列。版本分配只承认目标 escrow 合约 canonical block 中、反馈创建之后、匹配 task/assignment 且为最新值的事件；未确认事件返回 `425`，链重组后 canonical 连接消失并自动失去授权。
- 成功 Worker 回调必须逐项响应反馈并提交结构化差异。Engine 生成 `formal-proof-v1`，绑定任务、assignment、交付单元、套餐/范围、版本/聚合版本、work nonce、Agent、内容、父内容、反馈摘要、响应摘要、差异摘要、策略和截止时间，并使用独立服务端 secp256k1 密钥签名。
- 响应、差异、证明与版本 `review` 终态在同一事务写入，且全部禁止修改或删除。结果幂等哈希包含证明，因而相同回调可重放，重排后的反馈响应被规范化，而证明替换或内容变化会冲突。

## T-303 变更单生命周期与责任策略

### 生命周期与并发

`responsibility_pending -> awaiting_acceptance -> awaiting_funding | ready_to_activate -> effective -> consumed`。

- Publisher 提案必须绑定当前 V3/V4 `review` 版本、内容哈希、一次性反馈集、新规格哈希、结构化范围差异、追加价格和截止时间。目标版本由 Engine 固定为父版本加一，因此只可能是 V4/V5。
- Admin 或 Arbitrator 只能提交责任类型与原因；Engine 根据责任策略派生 `funding_source`、授权价格、本金所有者、余额接收者和资金账户，调用方不能覆盖这些字段。
- Publisher 对责任结果单独确认。资金责任路径只有在 `change_order_escrow` 权威子账余额不低于授权价格时才能生效；生效事务冻结账户并创建新的不可变范围快照。Agent 责任路径授权价格固定为零，不创建资金账户或任何 Agent 借记。
- 所有命令使用 actor + 幂等键、请求哈希、任务/变更单 advisory lock、聚合版本和数据库状态迁移共同串行化。提案、责任、认可、生效和消费事件均只追加。

### 责任与资金矩阵

| 责任 | 资金来源 | 授权价格 | 本金所有者 | 默认余额接收者 |
| --- | --- | ---: | --- | --- |
| Publisher 新增范围 | Publisher | 提案价格 | Publisher | Publisher |
| Agent 原范围修复 | Agent 自行吸收 | 0 | Agent Provider（仅责任元数据） | Agent Provider；无资金账户 |
| Platform 事故 | Platform 事故账户 | 提案价格 | `PLATFORM_INCIDENT_OWNER_ID` | Platform 事故账户；仅显式不可撤销补偿时为 Publisher |

数据库同时校验资金账户类型、任务/变更单引用及本金/余额归属。已生效资金账户进入 `frozen`，普通 funding reversal 不能移走已授权资金。

### V4/V5 执行边界

- V4/V5 启动仍需上一版本、反馈和在变更单生效之后确认的最新 canonical work nonce，并且必须原子消费目标版本唯一的 `effective` 变更单。
- 新执行命令绑定新范围哈希、新规格哈希、变更单 ID、责任和追加截止时间；交付证明也绑定变更单 ID。成功结果登记 `change_order` 计费身份及责任策略决定的授权价格。
- 状态机最大版本固定为 5，提案目标也被数据库限制为 4/5；因此不同幂等键、重试或已消费变更单都无法创建 V6。

### API

- `POST /v1/tasks/{taskId}/formal-change-orders`
- `POST /v1/tasks/{taskId}/formal-change-orders/{changeOrderId}/decision`
- `POST /v1/tasks/{taskId}/formal-change-orders/{changeOrderId}/accept`
- `POST /v1/tasks/{taskId}/formal-change-orders/{changeOrderId}/activate`
