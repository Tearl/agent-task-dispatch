# 托管、账本、结算与退款 — 任务

## 版本历史
| 版本 | 日期 | 变更 |
| --- | --- | --- |
| v1 | 2026-08-20 | 初始计划 |
| v2 | 2026-08-24 | SQ ledger 对账：T-401 至 T-405 无可验证完成收据，保留代码并恢复为 pending，待逐项重新验收 |
| v3 | 2026-08-24 | 增加发布后 FormalEscrow 托管入金桥接 remediation 任务 |
| v4 | 2026-08-24 | 加固 T-406 发布者派生 ID、交易 attempt 重试、确认绑定与过期退款隔离 |
| v5 | 2026-08-24 | 加固 T-406 链上正式预算承诺、canonical occurrence 收敛和历史金额隔离 |
| v6 | 2026-08-24 | 加固 T-406 广播先于 attempt 注册的 retained occurrence 对账 |
| v7 | 2026-08-24 | 明确 T-406 依赖已验收的账本、链投影与托管选择基础任务 |
| v8 | 2026-08-24 | 加固 T-406 同一 occurrence 再次 canonical 的 epoch 化资金恢复 |

## 任务

- [x] [T-401] [done] 实现隔离账户、allocation 与平衡不可变账本
  - Repository: .
  - Covers: F-401, F-402, AC-401
  - Depends on: T-101
  - Estimate: 10h
  - Risk: critical
  - Verification: 数据库约束与属性测试覆盖平衡、冲正、隔离、并发与重投

- [ ] [T-402] [pending] 实现幂等权威链上事件投影与对账
  - Repository: .
  - Covers: F-402, F-407, AC-404, AC-405
  - Depends on: T-401
  - Estimate: 10h
  - Risk: critical
  - Verification: 覆盖重放、乱序、重组、游标恢复、差异注入与审计

- [ ] [T-403] [pending] 实现托管选择与 work nonce 合约路径
  - Repository: .
  - Covers: F-401, F-403, AC-401, AC-402
  - Depends on: T-401
  - Estimate: 12h
  - Risk: critical
  - Verification: Foundry 单元、不变量、模糊、授权、重放、并发选择、抵扣与重入测试

- [ ] [T-404] [pending] 实现验收、结算、退款与收益隔离路径
  - Repository: .
  - Covers: F-404, F-405, F-406, AC-402, AC-403
  - Depends on: T-402, T-403
  - Estimate: 12h
  - Risk: critical
  - Verification: Foundry/Engine 集成测试覆盖每个资金路径、终态、重放与跨账攻击

- [ ] [T-405] [pending] 交付分离的资金、退款、收益与对账视图
  - Repository: .
  - Covers: F-401, F-405, F-406, AC-402, AC-405
  - Depends on: T-402, T-404
  - Estimate: 8h
  - Risk: high
  - Verification: BFF/Web 契约与可访问性测试区分已提交、待确认、已确认、可退和终态资金

- [ ] [T-406] [pending] [NEW v3] 桥接已发布任务的 FormalEscrow 托管入金与权威确认
  - Repository: .
  - Covers: F-401, F-402, F-407, F-408, AC-401, AC-404, AC-406
  - Depends on: T-107, T-401, T-402, T-403
  - Source relationship: remediation for the missing published-task integration across T-401 ledger, T-402 projection, and T-403 contract primitives; those broader tasks must be independently reviewed and verified before this bridge can start
  - Estimate: 12h
  - Risk: critical
  - Verification: Engine/PostgreSQL 集成测试覆盖意图所有权、EVM 金额域与历史隔离、确定性回放、追加式 attempt/occurrence、投影先于 attempt 注册及其并发重放、失败/替换/orphaned 收敛、同一 occurrence 的 canonical→orphaned→canonical epoch 恢复、交易哈希/receipt/calldata/事件绑定、过期退款隔离与重组冲正；Web/BFF 测试覆盖钱包广播后 `/submit` 失败重试、状态恢复、replacement、重复提交和确认前禁止匹配；Foundry 覆盖发布者域与正式预算共同派生 task ID、calldata 抢跑隔离、`msg.value` 精确相等、期限与重放
