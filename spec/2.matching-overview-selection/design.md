# 匹配、概览与 Agent 选择 — 设计

## 版本历史
| 版本 | 日期 | 变更 |
| --- | --- | --- |
| v1 | 2026-08-20 | 初始设计 |
| v2 | 2026-08-21 | 冻结 matching-rules-v1 的召回融合、评分与降级策略 |
| v3 | 2026-08-21 | 冻结 fair-shuffle-v1、匹配修订和不可变快照契约 |
| v4 | 2026-08-21 | 冻结 agent-execution-v1、逻辑执行/网络尝试分层和 fencing 回调契约 |
| v5 | 2026-08-22 | 冻结 overview-orchestration-v1、客观校验、计费投影和一次补位契约 |
| v6 | 2026-08-22 | 冻结 selection-reservation-v1、EIP-712 选择证明和链上 assignment 确认边界 |
| v7 | 2026-08-22 | 接入 T-402 权威链事件投影、确认深度和重组隔离 |
| v8 | 2026-08-22 | 交付 matching-view-v1、同源选择流程与链上确认 UI |

## 架构与影响层
Engine 匹配领域拥有资格、评分和随机化规则；适配器提供词法、分类、向量和模型信号；调度器拥有租约和执行；BFF/Web 只暴露快照与状态。

## 组件/模块边界
拆分过滤、召回、评分、调整、合格池、抽样、快照、概览编排、校验、allocation 与选择预留模块。

## API 或接口契约
版本化的匹配、概览和选择意图契约绑定任务/规格哈希、修订、Agent、报价、allocation、assignment、策略、链、合约、nonce 和过期时间。Agent 调用携带逻辑执行/幂等 ID 和签名回调数据。

## 数据模型与迁移
追加匹配修订、候选、分项得分、排除原因、概览执行/尝试/结果、allocation、选择预留和 assignment，业务身份必须唯一。

## 状态、失败、重试与并发
使用确定性 seed 派生与带权无放回抽样；使用 fencing token 租约容量；重投复用逻辑执行身份。选择先链下预留，仅链上确认后完成，交易失败释放预留。

## 安全与隐私
生成阶段限域的脱敏简报、只读策略和成本上限；校验回调真实性、时间戳与 nonce；快照取哈希，避免无必要记录任务内容。

## 可观测性与运维
监控过滤原因、召回降级、得分分布、探索、概览延迟/有效性/成本、补位、租约 fencing 与选择确认。

## 发布、兼容与回滚
算法和策略必须版本化；新版本启用前先影子运行；旧快照保持可读；模型调整可独立禁用。

## 技术决策与备选方案
规则评分保持权威，模型调整限制为 ±5。PostgreSQL 记录真相，Redis 仅用于租约协调和缓存。

### matching-rules-v1

- 硬过滤在任何召回之前执行；每个淘汰原因使用稳定 reason code。时间、预算、健康、容量、审核、风控、协议、收款地址与向量版本均不可由召回或模型绕过。
- 三路召回 adapter 均限制 Top-100，按 Agent ID 去重；融合采用 `k=60` 的 Reciprocal Rank Fusion，分值并列时按 Agent ID 升序，最多 100 个进入评分。
- 任务匹配 60 分拆为 dense 25、lexical 15、exact 20；召回相关性使用 `0..10000` 定点数，避免浮点与数据库返回顺序造成重放差异。
- 五维信誉各 5 分；正式价格预算余量与截止时间余量各 5 分；剩余容量 5 分。所有分项先截断到合法范围，再用整数计算。
- `ModelDelta` 截断为 `-5..+5`，`RankingScore` 截断为 `0..100`。模型关闭或失败时固定回退为 0 并记录降级。
- 合格池同时执行 `RuleScore >= 60`、`RankingScore >= 60`、距离本次最高 `RankingScore <= 10`，稳定排序为 `RankingScore DESC, RuleScore DESC, agent_id ASC`，最多 20 个且不补低分候选。
- dense、lexical、exact 或 ranking model 失败时只使用剩余信号并记录结构化 degradation；阈值和硬过滤保持不变。

### fair-shuffle-v1

- seed 使用版本化的至少 32 字节 HMAC-SHA256 密钥，由长度前缀编码的 `task_id + task_spec_hash + match_revision + algorithm_version + seed_key_version` 派生；只持久化 seed 摘要和密钥版本，绝不保存秘密。
- 权重固定为 `max(1, RankingScore - 60 + 1)`。候选先按 Agent ID 稳定排序，每个位置使用 HMAC 派生的无模偏整数抽取，记录精确的条件概率分子/分母和随机 draw，不使用浮点数。
- 最多逐位抽取 3 个且不放回；MVP 以 `ProviderID`（当前映射 Agent `owner_id`）执行默认每服务商 1 个上限。
- 探索判定固定为 1500 basis points。`ExposureCount < 100` 或 `EffectiveSamples < 20` 的合格 Agent 属于探索池；探索只允许第三位，没有可用探索候选时回退主池且不放宽质量门槛。
- 相同 seed 上下文和候选集合必须产生相同结果；候选输入顺序不参与随机结果。

### 匹配修订与快照

- 浏览器刷新和普通读取只调用 `Latest`，不得触发重算或 revision 变化。
- 权威匹配触发为所有有效输入生成 `effective_input_hash`。`task_id + task_spec_hash + algorithm_version + effective_input_hash` 相同则原样重放；变化时在任务级事务锁内递增 `match_revision`。
- PostgreSQL 保存完整快照正文和规范化候选审计行，包括排除原因、召回证据、分项得分、资格、权重、条件概率、位置、探索、降级和版本信息。
- 快照写入候选后立即封存；数据库触发器拒绝封存后的插入、更新和删除。回滚迁移同样不得删除历史快照。

### agent-execution-v1

- 健康检查沿用 Agent 注册模块的 HTTPS 协议探测；调度请求使用注册健康 URL 的同源 origin 作为执行 API base，不信任任务输入提供的任意目标。执行 API 固定为 `POST /v1/executions`、`/status`、`/cancel`、`/deliverable`，禁止重定向并限制响应为 64 KiB。
- 四类请求使用同一个版本化 envelope，绑定 `stage`、逻辑执行、网络尝试、Agent、任务与规格哈希、责任代码、成本上限、工具策略、截止时间、幂等键、回调 URL/nonce 和 fencing token。概览另绑定匹配修订、allocation、报价哈希；正式执行另绑定 assignment、套餐、版本、聚合版本和 work nonce。
- `logical_executions` 表示一次可计费业务工作，规格和幂等键创建后不可变；`execution_attempts` 只表示网络投递/租约尝试。同一不确定网络失败重投复用逻辑 ID、幂等键、attempt、fencing 和 callback nonce；明确拒绝或租约过期才创建递增的新 attempt。
- 调度容量复用 Agent 领域的 PostgreSQL 权威 lease。reservation ID 等于稳定 attempt ID；租约使用数据库时钟、单调 fencing token 和最大 1 小时 TTL。旧 attempt、旧 fencing 或已过期 lease 的回调只追加 `stale_fence` 证据，不推进业务状态。
- 回调入口固定为 `POST /v1/agent-callbacks/{logicalExecutionID}/{attemptID}`。路径身份必须与 JSON 正文一致；正文使用 Agent 分版本 HMAC-SHA256 密钥签名，校验时间戳偏差和一次性 nonce。数据库只保存 nonce/payload 摘要，不保存签名、明文 nonce 或 Agent 密钥。
- 同一 nonce、同一 payload 的回调原样重放；同一 nonce 对应不同 payload 返回冲突。取消、成本停止或终态后的合法迟到结果只追加 `late` 证据，不覆盖结果，也不触发结算。
- 状态查询的累计用量必须单调；达到成本上限即把权威用量截断为上限、终止 attempt、发送取消并释放容量。Agent 上报超过上限的部分不可成为事后收费依据。
- 成功回调冻结 `content_hash + deliverable_ref`；后续获取交付物必须与二者完全一致，避免相同幂等执行被替换内容。Agent 返回的自由文本拒绝原因不落库，只记录平台定义的稳定 reason code。

#### 状态与重投

| 场景 | 逻辑执行 | 网络尝试 | 处理结果 |
| --- | --- | --- | --- |
| 首次调度 | `pending -> running` | `prepared -> active` | 获取容量 lease 并投递 create |
| 网络结果不确定 | 保持 `running` | 保持同一 `active` | 原 envelope 重投 |
| Agent 明确拒绝 | `running -> failed` | `active -> failed` | 释放 lease；下次新建 attempt |
| lease 过期 | `running -> pending` | `active -> expired` | 新 attempt 和更大 fencing token |
| 主动取消 | `* -> cancel_requested -> cancelled` | `prepared/active -> cancelled` | 取消后回调仅审计 |
| 达到成本上限 | `running -> cost_stopped` | `active -> failed` | 截断费用、远端取消、释放 lease |
| 有效终态回调 | `running -> succeeded/failed` | `active -> completed/failed` | 一次性落结果并释放 lease |

`execution_callback_events` 为只增不改的回调证据；迁移回滚不得删除逻辑执行、网络尝试或回调历史。

### overview-orchestration-v1

- 一个已封存匹配快照最多创建一个概览批次。批次只读取快照，不触发重新评分或洗牌；初始 slot 严格来自快照的最多三个 `Selections`，所有 slot 使用同一个 Engine 截止时间。
- 概览模块不接受完整任务正文。`BriefProvider` 负责生成脱敏简报并返回非 bearer secret 的短期 `brief_ref + brief_hash`；执行协议同时绑定二者。实际请求鉴权由 T-104 的 Agent 凭证完成，数据库不得保存访问 token、签名或简报正文。
- 每个 slot 独立绑定 Agent、服务商、冻结价格版本、报价哈希、allocation、逻辑执行和只读工具白名单。目标解析器必须把当前 Agent 端点与快照中的 Agent、服务商、价格版本、概览价及外部成本上限逐项比对，避免跨候选或跨报价拼接。
- 最多三路 `Dispatch` 并行执行；Worker/HTTP 重投复用 T-203 的逻辑执行和网络 attempt。只有成功投递才把 slot 从 `planned` 推进为 `dispatched`，不确定网络错误保持可安全重投。
- allocation 的资金授权、捕获和释放属于 T-401；概览模块只通过 `AllocationGateway` 使用以 allocation ID 为幂等边界的 `authorize/capture/release`，并保存不可变绑定和状态投影，不复制余额或复式账本规则。

#### 客观校验与计费

概览交付物固定为 `overview-result-v1` JSON，拒绝未知字段和尾随文档，最大 64 KiB。有效结果必须同时满足：

- `content_hash` 与实际字节一致；
- 包含理解摘要、至少一项方法、交付结构、关键风险和合法预计时长；
- Engine 接收终态的时间不晚于批次截止时间；
- 平台工具审计证据完整、没有外部写尝试，且每个工具都在只读白名单中；
- 同一批次不存在已接受的相同内容哈希。

校验 reason code 使用稳定、排序后的集合。只有 `valid` slot 可以按冻结概览价调用 capture；无效、超时、取消、成本停止或越权结果只能 release。capture/release 与本地投影之间允许崩溃重试，但 T-401 必须以 allocation ID 保证不会重复计费。

#### overview-replacement-v1

- 每个批次最多补位一次，且只能在原 slot 客观无效后触发；主观不喜欢不得补位或拒付。
- 候选只来自原快照的 `Qualified` 稳定顺序，排除所有已经投递过的 Agent 和服务商，不重新召回、不放宽质量门槛。
- 补位 slot 使用新的 allocation 和逻辑执行 ID，同时保留原失败 slot 与 release 证据；并发失败通过批次事务锁收敛到同一个补位。
- 没有合格补位时永久记录 `replacement_exhausted`，普通重试不得再次尝试另一候选。

新匹配修订生效时，旧批次变为 `obsolete`。未终态执行被取消、未捕获 allocation 被释放；已经有效并捕获的概览费不追回，但旧批次不得进入 T-205 选择或抵扣。

### selection-reservation-v1

- 创建预留的唯一入口为 `POST /v1/tasks/{taskId}/selection-reservations`，必须携带发布者会话、`Idempotency-Key`、已完成概览批次和有效 slot。读取与对账接口同时绑定发布者、task 和 reservation，禁止仅凭 reservation ID 跨资源访问。
- Engine 在事务提交前重新验证任务仍为 `awaiting_selection`、批次属于最新封存修订、slot 客观有效且已捕获概览费、候选仍合格、Agent 当前健康且价格/控制器/收款地址未漂移、allocation 已捕获。任务级唯一索引保证并发选择只有一个有效预留。
- 预留先获取 Agent 容量 lease，并保存 fencing token 与不晚于任务截止时间的证明期限。数据库提交失败必须按 fencing token 释放 lease；确认、明确失败和证明到期释放容量。定时 worker 可用 `Expire` 幂等回收过期预留。
- 概览抵扣固定等于被选 slot 的已捕获概览价，`formal_payable = formal_package_gross_price - overview_credit`。不得由客户端提供价格、抵扣、Agent 地址、报价哈希或 allocation；Engine 只从不可变快照、价格版本和资金投影中重建。
- Engine 生成与 `TaskEscrow.SelectionProof` 字段顺序完全一致的 EIP-712 证明，domain 固定为 `AgentTaskEscrow / 1 / chainId / verifyingContract`。证明绑定 task、assignment、Agent 控制器、收款地址、overview、allocation、报价、规格、匹配修订、价格版本、毛价、抵扣、策略、唯一 nonce 和期限。
- 平台签名只在响应时由配置密钥重新生成，数据库只保存 payload hash、typed-data digest 和证明字段；不得保存签名或私钥。assignment、allocation 和 nonce 均唯一，链上正式净额锁定与多付退款由 T-403 合约执行。
- `POST /v1/tasks/{taskId}/selection-reservations/{reservationId}/reconcile` 只接受交易哈希。Engine 必须从权威链上 receipt/event 投影获得结果，并逐字段比对证明、正式净额和初始 `work_nonce=1`；浏览器提供的事件正文永远不可信。
- 链上确认后在同一 PostgreSQL 事务内追加不可变 assignment、把任务推进为 `assigned`、记录确认事件；重复确认返回同一 assignment。证明不匹配、交易哈希漂移或任务状态冲突不得推进任何状态。
- T-402 的 `authoritative-chain-projection-v1` 已作为生产 `ChainVerifier` 接入。未达到确认深度的交易保持 `submitted`；只有 canonical receipt、calldata 与日志一致时才确认。确认事件随后被重组孤立时，任务进入 `chain_reorg_pending`，不得自动重选。

#### 状态与幂等

| 场景 | reservation | assignment / 容量 |
| --- | --- | --- |
| 首次选择 | `reserved` | 返回确定性证明；持有 fencing lease |
| 同 key 同请求 | 原样重放 | 不重复签发身份或占用容量 |
| 同任务并发选择 | 一个 `reserved` | 失败竞争者释放其 lease |
| 交易待确认 | `reserved -> submitted` | 不创建 assignment，继续持有容量 |
| 权威确认 | `reserved/submitted -> confirmed` | 创建唯一 assignment，`work_nonce=1`，释放容量 lease |
| 权威失败 | `reserved/submitted -> failed` | 不创建 assignment，释放容量 lease |
| 证明到期 | `reserved/submitted -> expired` | 不创建 assignment，幂等释放容量 lease |

### matching-view-v1

- 发布者比较页以已发布的 `task_id` 为唯一入口。Engine 在校验发布者角色和任务所有权后，读取当前不可变规格对应的 `Latest` 封存快照；普通刷新不会触发匹配、洗牌或修订递增。
- 读模型只投影 FairShuffle 最终最多三个候选，包含位置、探索标志、冻结价格、预计时长、规则/模型分项、概览 slot 客观状态、计费状态、校验原因和内容哈希。浏览器不再导入模拟 Agent 或自行计算排名。
- 依赖降级以 `dependency + code + message` 原样展示；无快照、无合格候选、概览运行中、无效、补位和计费未完成均是显式状态。只有 `slot.status=valid` 且 `billing_status=captured` 的候选可以创建选择预留。
- BFF 仅提供同源任务绑定路由，校验 Engine 匹配响应并限制 selection reservation 路径；浏览器无法选择内部 Engine 地址。读取、预留、预留恢复和对账始终同时绑定 task 与 reservation。
- Web 使用同一个客户端 operation ID 重放预留。钱包交易 calldata 只由 Engine 返回的冻结 proof 和平台签名编码；提交前校验钱包 chain ID，不允许客户端改写价格、抵扣、地址、nonce 或期限。
- 钱包返回交易哈希后立即调用权威链对账。投影尚未观察到交易时，Engine 仍先幂等记录 `submitted + transaction_hash`，随后返回待确认；页面刷新可恢复同一交易。只有 canonical 确认才显示 assignment 已创建，失败、过期或 orphaned 均禁止再次提交旧证明。
