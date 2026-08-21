# 匹配、概览与 Agent 选择 — 设计

## 版本历史
| 版本 | 日期 | 变更 |
| --- | --- | --- |
| v1 | 2026-08-20 | 初始设计 |
| v2 | 2026-08-21 | 冻结 matching-rules-v1 的召回融合、评分与降级策略 |
| v3 | 2026-08-21 | 冻结 fair-shuffle-v1、匹配修订和不可变快照契约 |
| v4 | 2026-08-21 | 冻结 agent-execution-v1、逻辑执行/网络尝试分层和 fencing 回调契约 |

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
