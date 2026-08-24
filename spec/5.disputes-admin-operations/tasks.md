# 争议与管理运营 — 任务

## 版本历史
| 版本 | 日期 | 变更 |
| --- | --- | --- |
| v1 | 2026-08-20 | 初始计划 |
| v2 | 2026-08-23 | T-501 至 T-506 实现并完成 Engine/BFF/Web 验证 |
| v3 | 2026-08-24 | SQ ledger 对账：历史验证缺少可验证完成收据，保留代码并将 T-501 至 T-506 恢复为 pending，待逐项重新验收 |

## 任务

- [ ] [T-501] [pending] 实现争议资格、案件、主张、软锁与确认冻结
  - Repository: .
  - Covers: F-501, F-502, AC-501, AC-502
  - Depends on: T-303, T-404
  - Estimate: 10h
  - Risk: critical
  - Verification: 覆盖资格矩阵、所有权/状态、并发申请/反请求、重复与链上确认

- [ ] [T-502] [pending] 实现加密只追加证据清单与限域访问
  - Repository: .
  - Covers: F-503, AC-503
  - Depends on: T-501
  - Estimate: 9h
  - Risk: critical
  - Verification: 覆盖完整清单、WORM 失败、不可变、访问过期、冲突、加密元数据与审计

- [ ] [T-503] [pending] 实现裁决、和解、冲突校验与唯一复核
  - Repository: .
  - Covers: F-504, F-507, AC-501, AC-504
  - Depends on: T-502
  - Estimate: 10h
  - Risk: critical
  - Verification: 覆盖截止时间、职责分离、冲突、费用、档位、和解基点、重放与信誉更新时机

- [ ] [T-504] [pending] 实现冻结叶子争议分配合约路径
  - Repository: .
  - Covers: F-505, AC-505
  - Depends on: T-503, T-404
  - Estimate: 12h
  - Risk: critical
  - Verification: Foundry 单元/不变量/模糊测试覆盖完整性、稳定索引、所有者/上限/费用/舍入绑定、排除与价值守恒

- [ ] [T-505] [pending] 实现可审计的管理、DLQ 与对账修复操作
  - Repository: .
  - Covers: F-506, AC-506
  - Depends on: T-402, T-503
  - Estimate: 9h
  - Risk: high
  - Verification: 覆盖权限、密钥拒绝、仅追加/冲正、幂等重放与不可变审计

- [ ] [T-506] [pending] 交付争议与管理工作流
  - Repository: .
  - Covers: F-501, F-502, F-503, F-504, F-506, AC-502, AC-503, AC-504, AC-506
  - Depends on: T-503, T-504, T-505
  - Estimate: 10h
  - Risk: high
  - Verification: BFF/Web 契约、角色隔离、可访问性、时间线、链上待确认、证据访问与修复审计测试
