# 身份、Agent 与任务基础 — 设计

## 版本历史
| 版本 | 日期 | 变更 |
| --- | --- | --- |
| v1 | 2026-08-20 | 初始设计 |
| v2 | 2026-08-21 | 确定 Agent 地址首次激活后永久冻结 |

## 架构与影响层
`apps/web` 渲染角色流程并签署钱包挑战；`apps/bff` 管理 Cookie 和响应聚合；`services/engine` 拥有授权、状态、幂等和持久化。PostgreSQL 保存用户、钱包、角色、Agent、价格版本、nonce 消费、任务/规格版本、迁移和 Outbox。

## 组件/模块边界
Engine 拆分领域、传输、仓储、信封加密、审计与 Outbox。BFF 通过内部认证通道传递身份和幂等上下文，不重复 Engine 规则。

## API 或接口契约
会话路由覆盖 nonce、登录/登出和会话；Engine 契约覆盖 Agent CRUD/凭证轮换/健康/价格与任务草稿/规格/发布/可执行操作。变更需携带 `Idempotency-Key` 及适用的聚合版本，重放返回稳定结果。

## 数据模型与迁移
使用不可变的 `wallet_nonces`、`agent_price_versions`、`task_spec_versions`、`acceptance_versions`、`domain_events`、`audit_events` 和 `outbox`；可变聚合只引用当前版本。凭证仅存密文、密钥引用和指纹。

[NEW v2] Agent 聚合保存只增不减的 `activated_at`；该字段首次进入 `active` 时设置且永不清空，作为控制者和收款地址永久冻结的权威依据。

## 状态、失败、重试与并发
nonce 消费与会话创建原子执行。Agent/任务变更通过聚合版本比较或加锁。保存幂等请求哈希和响应，同一键用于不同输入时必须拒绝。发布与 Outbox 插入共用事务。

[NEW v2] 控制者/收款地址更改必须同时校验所有者、聚合版本、幂等键、当前状态与 `activated_at IS NULL`。`active`、已激活后的 `paused` 及 `retired` 都拒绝修改，不得清空 `activated_at` 来解冻。

## 安全与隐私
采用 EIP-4361 兼容语义或明确版本化的等价方案，脱敏签名、凭证和会话值，在 Engine 服务/仓储层执行所有权和角色校验。

## 可观测性与运维
统计认证失败原因、授权拒绝、过期写入、重放、凭证轮换、Agent 健康和 Outbox 延迟。高风险操作审计不包含密钥载荷。

## 发布、兼容与回滚
先执行可加性迁移，再按环境开启端点。回滚时禁用路由，但保留不可变历史。

## 技术决策与备选方案
PostgreSQL 唯一约束为应用幂等提供最终保障。Redis 可加速 nonce 查询，但不得是唯一记录。厂商特定 KMS 隐藏在信封加密接口后。

[NEW v2] 地址冻结使用单调 `activated_at` 而非仅依赖当前状态，避免通过 `active → paused` 绕过冻结。
