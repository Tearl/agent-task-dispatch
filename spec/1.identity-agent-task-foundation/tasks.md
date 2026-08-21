# 身份、Agent 与任务基础 — 任务

## 版本历史
| 版本 | 日期 | 变更 |
| --- | --- | --- |
| v1 | 2026-08-20 | 初始计划 |
| v2 | 2026-08-21 | 纳入 Agent 地址首次激活后永久冻结决策 |

## 任务

- [x] [T-101] [done] 建立 Engine 持久化、迁移、幂等、审计与 Outbox 基础
  - Repository: .
  - Covers: F-102, F-107, AC-102, AC-106
  - Depends on: none
  - Estimate: 6h
  - Risk: high
  - Verification: 运行 `pnpm --filter @agent-platform/engine test`，覆盖迁移、重复键和 Outbox 原子性测试

- [x] [T-102] [done] 实现钱包 nonce 校验与会话契约
  - Repository: .
  - Covers: F-101, F-102, AC-101, AC-102
  - Depends on: T-101
  - Estimate: 6h
  - Risk: high
  - Verification: Engine/BFF 认证测试覆盖过期、重放、域名、链、角色刷新与脱敏

- [x] [T-103] [done] 实现 Agent 所有权、生命周期、健康、容量和不可变价格
  - Repository: .
  - Covers: F-102, F-103, AC-102, AC-103
  - Depends on: T-101, T-102
  - Estimate: 8h
  - Risk: high
  - Verification: Engine 授权/状态测试、数据库价格约束与并发容量测试

- [x] [T-104] [done] 实现只写加密的 Agent 凭证轮换
  - Repository: .
  - Covers: F-104, AC-104
  - Depends on: T-103
  - Estimate: 5h
  - Risk: high
  - Verification: 覆盖凭证写入、所有权边界、管理员拒绝、响应快照与日志脱敏

- [x] [T-105] [done] 实现任务草稿与不可变规格发布
  - Repository: .
  - Covers: F-102, F-105, F-107, AC-102, AC-105, AC-106
  - Depends on: T-101, T-102
  - Estimate: 8h
  - Risk: high
  - Verification: 覆盖状态迁移、所有权、哈希、乐观并发、幂等与重投

- [x] [T-106] [done] 提供 BFF 聚合与 available-actions 契约
  - Repository: .
  - Covers: F-106, AC-105
  - Depends on: T-103, T-105
  - Estimate: 5h
  - Risk: medium
  - Verification: BFF 契约测试证明浏览器隔离及不可执行原因来自 Engine

- [ ] [T-107] [pending] 接入可访问的身份、Agent 和任务 Web 流程
  - Repository: .
  - Covers: F-101, F-103, F-105, F-106, AC-101, AC-103, AC-105
  - Depends on: T-102, T-104, T-106
  - Estimate: 8h
  - Risk: medium
  - Verification: Web 类型检查/构建，以及键盘、钱包错误、重复提交和响应式流程测试
