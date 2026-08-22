# 托管、账本、结算与退款 — 任务

## 版本历史
| 版本 | 日期 | 变更 |
| --- | --- | --- |
| v1 | 2026-08-20 | 初始计划 |

## 任务

- [x] [T-401] [done] 实现隔离账户、allocation 与平衡不可变账本
  - Repository: .
  - Covers: F-401, F-402, AC-401
  - Depends on: T-101
  - Estimate: 10h
  - Risk: critical
  - Verification: 数据库约束与属性测试覆盖平衡、冲正、隔离、并发与重投

- [x] [T-402] [done] 实现幂等权威链上事件投影与对账
  - Repository: .
  - Covers: F-402, F-407, AC-404, AC-405
  - Depends on: T-401
  - Estimate: 10h
  - Risk: critical
  - Verification: 覆盖重放、乱序、重组、游标恢复、差异注入与审计

- [x] [T-403] [done] 实现托管选择与 work nonce 合约路径
  - Repository: .
  - Covers: F-401, F-403, AC-401, AC-402
  - Depends on: T-401
  - Estimate: 12h
  - Risk: critical
  - Verification: Foundry 单元、不变量、模糊、授权、重放、并发选择、抵扣与重入测试

- [x] [T-404] [done] 实现验收、结算、退款与收益隔离路径
  - Repository: .
  - Covers: F-404, F-405, F-406, AC-402, AC-403
  - Depends on: T-402, T-403
  - Estimate: 12h
  - Risk: critical
  - Verification: Foundry/Engine 集成测试覆盖每个资金路径、终态、重放与跨账攻击

- [x] [T-405] [done] 交付分离的资金、退款、收益与对账视图
  - Repository: .
  - Covers: F-401, F-405, F-406, AC-402, AC-405
  - Depends on: T-402, T-404
  - Estimate: 8h
  - Risk: high
  - Verification: BFF/Web 契约与可访问性测试区分已提交、待确认、已确认、可退和终态资金
