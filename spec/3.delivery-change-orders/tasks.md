# 正式交付与变更单 — 任务

## 版本历史
| 版本 | 日期 | 变更 |
| --- | --- | --- |
| v1 | 2026-08-20 | 初始计划 |

## 任务

- [ ] [T-301] [pending] 实现冻结范围与正式版本状态机
  - Repository: .
  - Covers: F-301, F-304, AC-301, AC-302
  - Depends on: T-203, T-205
  - Estimate: 10h
  - Risk: high
  - Verification: 迁移与并发测试覆盖 V1–V3、单活动执行、重试和范围不可变

- [ ] [T-302] [pending] 实现结构化反馈、差异、证明与 work nonce 门控
  - Repository: .
  - Covers: F-302, F-303, AC-302, AC-303
  - Depends on: T-301, T-403
  - Estimate: 9h
  - Risk: critical
  - Verification: 覆盖父版本/哈希、过时聚合、work nonce、重复回调、证明替换与链上确认

- [ ] [T-303] [pending] 实现变更单生命周期与责任策略
  - Repository: .
  - Covers: F-304, F-305, F-306, AC-304, AC-305
  - Depends on: T-302, T-401
  - Estimate: 10h
  - Risk: critical
  - Verification: 覆盖全部责任/资金组合、并发生效、V4/V5 授权和永久拒绝 V6

- [ ] [T-304] [pending] 集成验收与结算资格
  - Repository: .
  - Covers: F-303, F-305, AC-303, AC-304
  - Depends on: T-303, T-404
  - Estimate: 8h
  - Risk: critical
  - Verification: 覆盖意图/待确认/已确认、过时证明、V1 提前验收与结算集成

- [ ] [T-305] [pending] 交付正式时间线、反馈、差异与变更单 UI
  - Repository: .
  - Covers: F-302, F-303, F-305, AC-302, AC-303, AC-304
  - Depends on: T-302, T-303, T-304
  - Estimate: 9h
  - Risk: medium
  - Verification: Web/BFF 契约、可访问性、链上待确认、拒绝原因、差异与重复提交测试
