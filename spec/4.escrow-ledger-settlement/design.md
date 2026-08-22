# 托管、账本、结算与退款 — 设计

## 版本历史
| 版本 | 日期 | 变更 |
| --- | --- | --- |
| v1 | 2026-08-20 | 初始设计 |
| v2 | 2026-08-22 | 冻结 double-entry-v1、隔离业务子账和 overview allocation 契约 |
| v3 | 2026-08-22 | 冻结 escrow-selection-v1、平台证明、概览抵扣和 work nonce 契约 |
| v4 | 2026-08-22 | 冻结 authoritative-chain-projection-v1、重组隔离与日对账契约 |
| v5 | 2026-08-22 | 冻结 acceptance-settlement-v1、Agent pull-payment 与可逆结算分录 |
| v6 | 2026-08-22 | 交付 finance-view-v1 发布者资金、Agent 收益与管理员对账读模型 |

## 架构与影响层
`contracts/escrow` 拥有资产和账户状态。Engine 拥有意图/状态校验、不可变复式记账、交易参数与事件投影。BFF/Web 展示分离余额与确认状态。

## 组件/模块边界
合约拆分账户/allocation、授权、选择、工作、验收、退款、提现和暂停；Engine 拆分分录、链游标/投影、对账和收益。

## API 或接口契约
每个资金命令/事件携带账户类型/ID、资产、金额、策略版本、领域对象、nonce 和幂等身份。签名意图绑定链 ID、合约与过期时间。

## 数据模型与迁移
追加账户、allocation、journal/entry、交易意图、链上事件/游标、确认历史、冲正、应收、收益仓位与对账运行/差异。

## 状态、失败、重试与并发
唯一消费键与合约 nonce 保证一次效果。投影器使用交易/日志身份和可逆确认状态。重组补偿追加新投影/冲正事件，永不删除历史。

## 安全与隐私
应用最小权限、CEI、安全代币转账、重放/域保护、不变量校验和紧急暂停。管理员不能签署发布者操作。

## 可观测性与运维
对分录不平、确认停滞、游标缺口、重组、余额不一致、退款/结算/提现失败和暂停事件告警。

## 发布、兼容与回滚
只在版本化决策后部署到选定测试链。合约升级/迁移需单独授权；链下模式保持可加性与可重放。

## 技术决策与备选方案
不内置生产链、资产、费率或生息假设。资金冲突以合约状态为准，PostgreSQL 保存可审计业务账务。

### double-entry-v1

- `DiscoveryPool`、`FormalEscrow`、`ChangeOrderEscrow`、`DisputeFeePool` 使用独立 `account_id + account_type + reference_id + asset_key`。账户冻结权益人、剩余接收人和退款策略版本；业务账户余额不得为负。
- `asset_key` 必须由部署配置显式提供，不内置生产链、代币或精度。Engine 启动装配必须使用同一资产键构造资金服务和概览 `AllocationGateway`。
- 余额是不可变分录的同步投影，只能由数据库 entry trigger 更新。journal 和 entry 禁止更新、删除；更正只能创建唯一 `reversal_of`，且逐账户、方向、金额和资产精确反转原 journal。
- 延迟约束触发器在事务提交时验证每个 journal 至少两笔、只有一种资产且 debit/credit 总额相等，因此分录可批量插入，但不平事务不能提交。
- 本账本使用“debit 从账户扣减、credit 向账户增加”的业务子账语义。系统控制账户允许负余额以镜像链上资金来源；四类业务账户永久禁止透支。

### overview allocation

- `overview` allocation 绑定当前匹配快照、任务规格哈希、修订、Agent、价格版本、报价哈希、`DiscoveryPool` 和资产。PostgreSQL 再次核对该 Agent 是快照中的 qualified 候选且冻结价格一致。
- 授权预留 `overview_price + external_cost_cap`；可用余额等于账户账面余额减全部 authorized allocation。账户行锁与稳定 advisory lock 保证并发授权不会超卖。
- 有效结果只允许捕获冻结概览价和不超过上限的实际外部成本；未使用预留自动释放。无效、超时、取消或过时修订只允许 release。capture 与 release 终态互斥并以 allocation ID/请求哈希稳定重放。
- `overview_capture` journal 必须从 allocation 绑定的 `DiscoveryPool` debit；概览金额只能 credit 到对应 Agent 应收账户，外部成本只能 credit 到该 allocation 专属清算账户。数据库提交约束拒绝跨账、跨 Agent 或金额拼接。
- allocation 状态变化另写不可变事件。零价格且零成本的有效结果可以进入 captured 终态，但不制造零金额 journal。
- `funds.OverviewGateway` 是 T-204 `overview.AllocationGateway` 的适配器；概览编排不拥有余额、预留、账本或冲正规则。

### escrow-selection-v1

- 当前 `TaskEscrow` 是本地 MVP 的原生资产实现；生产资产、精度、签名人治理和升级方式仍由阶段 0 决策，不得把本地部署参数当成生产默认值。
- 发布者直接提交选择交易即构成发布者授权；独立的平台证明签名人签署同一组选择字段。证明绑定任务、assignment、Agent 控制者、收款地址、概览、allocation、报价、任务规格哈希、匹配修订、价格版本、概览价格、正式总价、抵扣、策略、nonce、有效期、链 ID 和合约地址。
- EIP-712 domain 固定为 `AgentTaskEscrow` / `1`。结构为 `SelectionProof(bytes32 payloadHash)`，其中 `payloadHash = keccak256(abi.encode(SelectionProof))`，字段顺序以合约结构定义为准；签名拒绝高 `s`、非法 `v` 和零恢复地址。
- 一个任务只能从 `Funded` 原子进入一次 `Assigned`。选择消费全局唯一非零 nonce；assignment ID 和概览 allocation 抵扣身份同样只能消费一次。成功选择创建不可替换 assignment，并同时初始化 `work_nonce = 1`；失败交易回滚所有消费标记、assignment 和资金变化。
- `overview_credit <= overview_price` 且 `overview_credit <= formal_gross_price`；`formal_payable = formal_gross_price - overview_credit`。合约只锁定正式净应付并把超额正式预算退回发布者，不移动或追回已结算概览费。
- 超额退款遵循 checks-effects-interactions 并受重入锁保护。发布者推进新工作时必须携带观察到的当前 work nonce，使用 compare-and-swap 精确递增一次；过时或并发交易失败，不允许跳号。

### authoritative-chain-projection-v1

- 投影范围由部署配置显式绑定 `chain_id + contract_address + deployment_block`。启动时及每次同步均校验 RPC chain ID；生产 RPC 必须使用 HTTPS，本地私有 HTTP 只能通过显式开关启用。HTTP 客户端禁止重定向、限制响应体并设置超时。
- 运行时配置为 `EVM_RPC_URL`、`ESCROW_CONTRACT_ADDRESS`、`ESCROW_DEPLOYMENT_BLOCK`，可用 `EVM_CONFIRMATIONS` 和 `EVM_MAX_REORG_DEPTH` 覆盖默认 12/64；`EVM_RPC_ALLOW_PRIVATE_HTTP=true` 仅用于明确授权的本地链。
- `confirmation_depth=N` 表示区块至少获得 N 次确认后才进入权威投影。投影器严格按区块号顺序推进，不接受缺口、错误父哈希或跨合约日志；游标保存在 PostgreSQL，进程重启从最后权威区块继续。
- 区块、交易、日志和每次 canonical/orphaned 状态转换全部只追加。`chain_canonical_blocks` 与游标只是可重建的当前链协调索引；重组删除的仅是该索引映射，历史区块、交易、日志和状态证据永不删除。
- 交易消费身份为 `chain + contract + block_hash + transaction_hash`，日志消费身份额外绑定全局 `log_index`。同一区块重放不产生第二次效果；不同区块中的同一交易保留为不同历史 occurrence，并且只有 canonical occurrence 可供业务读取。
- 已知 `TaskEscrow` 事件使用冻结 ABI topic 解码。`SelectionConfirmed` 必须同时满足成功 receipt、恰好一个选择日志和 `selectAgent` calldata；证明、日志中的 task/assignment/地址/allocation/价格/抵扣/净额/work nonce 任一不一致即拒绝整个区块，不生成 assignment 权威结果。
- PostgreSQL 不保存交易 calldata 或平台签名，只保存 calldata SHA-256、是否为选择调用、解码后的无签名证明和事件字段。T-205 `ChainVerifier` 只读取达到确认深度的 canonical 投影；未知或尚未确认交易保持 pending，失败 receipt 返回稳定失败状态。
- 重组在配置的最大回溯深度内寻找共同祖先，然后按高度回退 canonical 索引并追加 orphaned 状态。已生成的 assignment 历史不删除；当前 assignment 映射被撤销，reservation 标记 `orphaned`，任务进入 `chain_reorg_pending`，禁止自动重选或继续正式工作，避免旧证明仍有效时产生双 assignment。
- 超过最大回溯深度时投影器停止并告警，不猜测共同祖先。运营人员必须先审计 RPC/链状态，再通过后续 T-505 管理操作恢复。

#### 日对账

- 每 24 小时在最新安全区块使用带 block tag 的 `eth_call` 比较合约 task amount、assignment ID、work nonce 与 PostgreSQL 当前 assignment/正式净额投影。
- canonical 链存在但本地缺失的 assignment、本地存在但链上缺失或不同的 assignment、金额/nonce 漂移及 reorg quarantine 均生成差异。比较键稳定排序，保证同一注入差异产生相同结果。
- 每次运行和每条差异均不可变保存；状态为 `matched` 或 `difference_detected`，差异包含类别、资源、期望值、观测值和严重级别。检测到差异只告警和隔离，不直接篡改不可变账本。
- T-404 新增释放、退款、收益后，在同一 inventory 接口继续加入金库和应收维度，不改变区块/事件消费身份或重组语义。

### acceptance-settlement-v1

- `accept(task_id)` 仅允许发布者在 `Assigned` 执行一次；兼容入口 `release` 使用同一状态转换。V1 验收把整笔 `formal_payable` 从任务托管转为 assignment 绑定的收益，不按未使用 work nonce/套餐轮次退款。
- 验收采用 pull-payment：合约先清零任务本金并进入 `Released`，再把金额累计到 `claimableEarnings[agent_controller][payout]`。验收不调用 Agent；只有绑定 controller 能发起提现，且接收方只能是证明绑定的 payout。部分提现先扣减可提现余额再转账，并受同一重入锁保护。
- 已验收本金离开任务域后，退款与争议入口因终态检查无法触及它；其他任务退款只消费各自 `task.amount`。Agent 争议授权绑定 controller 而非 payout，裁决给 Agent 时同样先进入隔离收益，裁决者不能绕过本人提现路径。
- 生息资格只是本金 inventory 与事件契约，不内置生产收益适配器。资金创建时不具备资格；只有明确选择后 `formal_payable` 进入资格，验收、退款或争议终态时退出。生产金库、风险和收益政策仍需阶段 0 决策。
- Engine 解码 `EarningsAccrued`、`EarningsWithdrawn` 和 `YieldEligibilityChanged`。canonical 验收原子追加 `formal_escrow → formal_agent_receivable`，提现追加 `formal_agent_receivable → funding_control`，发布者争议退款追加 `formal_escrow → funding_control`；正式收益账户以 `controller+payout+asset` 隔离，不与已结算概览应收混账。journal ID 直接绑定唯一链事件，重复消费不产生第二笔分录。
- settlement journal 与 entry 保持不可变和平衡。链重组先为受影响 journal 追加逐账户、方向、金额、资产精确反转的 reversal，再撤销 canonical 索引，并把关联任务置为 `chain_reorg_pending`；历史事件和原分录均不删除。
- canonical 视图分别投影任务结算、controller+payout 可提现收益和任务生息本金。日对账在安全区块额外调用 `claimableEarnings` 与 `yieldEligiblePrincipal`，因此合约余额、正式子账、Agent 应收和生息资格任一漂移都会形成独立差异。

### finance-view-v1

- Engine 提供三个只读聚合：发布者资金、Agent 收益和管理员对账。发布者查询只读取本人任务及其业务子账；Agent 查询只读取本人 Agent 的概览应收、正式应收和 canonical 链上可提现额；对账运行和差异只允许 `admin` 读取。权限在 Repository 查询前由 Engine 强制执行。
- 浏览器只请求同源 BFF `/api/finance/*`；BFF 使用 HttpOnly session 调用内部 Engine，并对响应结构和状态枚举做运行时校验。BFF 不重算余额、不推导领域状态，也不会向浏览器透传私钥、签名或凭据字段。
- 提交状态与确认状态是两个字段：`not_submitted/submitted` 和 `not_observed/pending/confirmed/failed/orphaned`。退款状态 `available/pending/confirmed/unavailable` 与任务 `terminal` 另行展示，禁止用一个模糊状态折叠交易已提交、链上待确认、可退款或领域终态。
- 发布者视图分别列出 DiscoveryPool、FormalEscrow、ChangeOrderEscrow、DisputeFeePool、可退余额与退款记录；Agent 视图区分 overview receivable、formal claimable 和 canonical chain claimable，并展示不可替换的 controller → payout 绑定；管理员视图只读展示 safe block、运行结果和逐项差异，不提供事件重放或账本改写按钮。
- 管理后台不再使用浏览器本地认证标记。管理员通过与普通用户相同的钱包签名建立服务端会话，前端路由守卫只用于体验，最终权限仍由 Engine 的权威 `admin` 角色决定；退出时统一撤销服务端会话。
