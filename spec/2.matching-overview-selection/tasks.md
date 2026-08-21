# 匹配、概览与 Agent 选择 — 任务

## 版本历史
| 版本 | 日期 | 变更 |
| --- | --- | --- |
| v1 | 2026-08-20 | 初始计划 |

## 任务

- [x] [T-201] [done] 实现硬过滤、召回适配器、评分与降级策略
  - Repository: .
  - Covers: F-201, F-202, AC-201
  - Depends on: T-103, T-105
  - Estimate: 10h
  - Risk: high
  - Verification: 使用确定性表驱动测试覆盖每个过滤项、得分项、阈值和依赖失败

- [x] [T-202] [done] 实现确定性 FairShuffle 与不可变匹配快照
  - Repository: .
  - Covers: F-202, F-203, AC-201, AC-202
  - Depends on: T-201
  - Estimate: 7h
  - Risk: high
  - Verification: 属性测试覆盖稳定性、带权唯一、服务商上限、探索位置和修订逻辑

- [x] [T-203] [done] 实现 Agent 执行协议与带 fencing 的调度租约
  - Repository: .
  - Covers: F-204, F-205, AC-203, AC-204
  - Depends on: T-104, T-202
  - Estimate: 10h
  - Risk: high
  - Verification: 覆盖协议、签名回调、成本上限、租约 fencing、取消、重投与迟到结果

- [ ] [T-204] [pending] 实现概览编排、校验、计费结果与补位
  - Repository: .
  - Covers: F-204, F-205, AC-203, AC-204
  - Depends on: T-203, T-401
  - Estimate: 10h
  - Risk: high
  - Verification: 三路 fan-out 测试覆盖截止时间、只读策略、客观有效性、重试、补位与 allocation 唯一性

- [ ] [T-205] [pending] 实现选择预留与链上确认 assignment
  - Repository: .
  - Covers: F-206, AC-205
  - Depends on: T-204, T-403
  - Estimate: 10h
  - Risk: critical
  - Verification: Engine/合约集成测试覆盖并发选择、证明不匹配、重放、抵扣、净额锁定与预留释放

- [ ] [T-206] [pending] 交付匹配、概览比较与选择 UI
  - Repository: .
  - Covers: F-203, F-204, F-206, AC-202, AC-203, AC-205
  - Depends on: T-202, T-204, T-205
  - Estimate: 8h
  - Risk: medium
  - Verification: BFF/Web 契约、可访问性、降级状态、交易待确认和重复提交测试
