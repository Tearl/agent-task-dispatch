# 推荐算法与 FairShuffle 洗牌逻辑分析

## 1. 文档目的

本文梳理当前项目中 Agent 推荐、排序和 FairShuffle 的真实状态，回答以下问题：

- 当前页面展示的推荐结果是如何计算的；
- 需求文档中规划的生产推荐链路是什么；
- FairShuffle 要解决什么问题，已有约束是什么；
- 哪些公式和参数尚未定义，不能直接进入生产实现；
- Engine 后续实现时建议采用什么模块边界和验证方法。

本文是代码和规格的分析记录；各章节会明确区分已实现的版本化算法与仍待决的建议。

## 2. 核心结论

项目目前存在两套不同层级的推荐逻辑：

1. **Web 演示排序已经存在**：`apps/web/src/app/pages/publisher/AgentRecommendations.tsx` 使用 3 条静态 Agent 数据，在浏览器中根据分类、预算和交付时间做简单加减分，然后按分数降序排列。
2. **推荐领域管线已经实现**：T-201、T-202 已在 Engine 增加硬过滤、三路召回 adapter、RRF 融合、100 分规则评分、模型有限调整、合格池、`fair-shuffle-v1`、匹配修订和不可变快照。生产候选查询与 BFF/Web 接入仍待后续任务完成。

因此，当前 Web 上的“综合匹配分”和“最佳匹配”只能视为交互原型，不能视为平台权威推荐结果，更不具备公平洗牌和审计能力。

## 3. 当前实际运行的推荐逻辑

### 3.1 数据来源

候选数据来自前端静态常量 `AGENTS`，当前只有 3 个 Agent：

| Agent | 初始 match | 分类 | 价格 | 预计时间 |
| --- | ---: | --- | ---: | --- |
| DataForge | 96 | 数据分析 | 1120 | 约 2 天 |
| InsightBot | 91 | 市场研究 | 1260 | 约 3 天 |
| QuantScope | 87 | 数据分析 | 990 | 约 2.5 天 |

初始 `match`、成功率、五维信誉和推荐理由都属于 mock 数据，不是从 Engine、历史任务或信誉系统计算得到的。

### 3.2 当前前端评分公式

当前 `rankAgents(analysis)` 的逻辑可以表示为：

```text
score = agent.match

if agent.category == task.category:
    score += 3
else:
    score -= 4

if agent.price <= task.budget:
    score += 1
else:
    score -= min(12, ceil((agent.price - task.budget) / 100))

if parsedEtaDays > 0 and parsedEtaDays <= task.deliveryDays:
    score += 1
else:
    score -= 3

finalScore = clamp(score, 60, 100)
results = sortDescending(finalScore)
```

同时，分类、预算、交付期匹配时会向推荐理由列表前方追加说明，最后对理由文本去重。

### 3.3 示例

假设任务为“数据分析”、预算 1200 USDC、要求 3 天内完成：

| Agent | 初始分 | 分类调整 | 预算调整 | 时间调整 | 最终分 |
| --- | ---: | ---: | ---: | ---: | ---: |
| DataForge | 96 | +3 | +1 | +1 | 100（由 101 截断） |
| QuantScope | 87 | +3 | +1 | +1 | 92 |
| InsightBot | 91 | -4 | -1 | +1 | 87 |

最终顺序是 DataForge、QuantScope、InsightBot。

### 3.4 当前逻辑的性质和局限

当前逻辑具备：

- 对任务分类、预算和时限的即时反馈；
- 确定性结果，相同页面输入通常得到相同顺序；
- 简单的推荐理由展示。

但它不具备生产推荐所需的关键能力：

- 没有状态、审核、健康有效期、容量、风控等硬过滤；
- 没有语义、关键词、分类三路召回；
- 没有根据五维信誉重新计算 100 分规则分；
- 没有 CTR/LTR 模型调整；
- 没有 60 分门槛和“距最高分不超过 10 分”的合格池；
- 没有带权随机、不放回抽样、服务商去重和 15% 探索；
- 没有匹配修订、算法版本、seed 或不可变快照；
- 评分最低被强制截断为 60，意味着不合格 Agent 在 UI 中也不会显示低于合格线的分数；
- 排序发生在浏览器，用户可修改本地代码和数据，不能作为权威业务决定。

特别需要注意：当前的 `clamp(score, 60, 100)` 与生产需求中的 `RuleScore >= 60` 门槛语义冲突。生产实现必须先计算真实分数，再淘汰低于门槛的 Agent，不能把低分强制改成 60。

## 4. 规划中的生产推荐链路

根据需求文档第 7.4 节和 `spec/2.matching-overview-selection`，生产链路应严格按以下顺序执行：

```text
已托管且可匹配的任务规格
        │
        ▼
    1. 硬过滤
        │
        ▼
    2. 三路召回
        │
        ▼
    3. 融合去重
        │
        ▼
    4. RuleScore（100 分）
        │
        ▼
    5. 模型有限调整（±5）
        │
        ▼
    6. 合格池筛选（最多 20）
        │
        ▼
    7. 确定性 FairShuffle
        │
        ▼
    最多 3 个候选 + 不可变快照
```

匹配规则必须由 `services/engine` 执行。Web 和 BFF 只能展示 Engine 返回的快照、分数、解释和不可执行原因。

### 4.1 硬过滤

硬过滤的作用是先处理“是否有资格”，不参与软排序。需求要求至少覆盖：

- Agent 状态必须可匹配，通常为 `active`；
- Agent 已完成审核；
- 健康检查仍在 5 分钟有效期内；
- 存在剩余执行容量；
- 分类满足任务要求；
- Agent 协议版本兼容；
- 概览价格、正式价格和外部成本不突破任务预算；
- 预计执行时间满足截止时间；
- 支持任务语言；
- 风控状态允许参与；
- 控制者和收款地址有效；
- 向量版本与当前检索版本兼容；
- Agent 所有者不得与任务发布者为同一主体或关联禁配主体。

任何召回依赖或模型故障都不能绕过硬过滤。每个被排除 Agent 应记录稳定的 reason code，而不仅是自然语言。

当前数据库已经保存部分过滤字段，如状态、健康有效期、容量、分类、语言、价格版本、控制者和收款地址，并存在针对 `active + healthy` Agent 的部分索引。但审核状态、风控状态、向量版本和服务商关联关系仍需要补充或明确。

### 4.2 三路召回

通过硬过滤后，规划中的召回包括：

1. **稠密语义召回**：将任务规格与 Agent 能力描述编码为向量，通过 Qdrant 取 Top-100。
2. **标签/关键词召回**：根据任务标签、能力词、工具和领域术语取 Top-100。
3. **分类/必需项精确召回**：根据分类、语言、必需能力和协议等结构化字段取 Top-100。

三路结果按 Agent 业务 ID 合并去重，最多让 100 个 Agent 进入评分阶段。

`matching-rules-v1` 已冻结使用 `k=60` 的 RRF（Reciprocal Rank Fusion）完成融合，分值并列按 Agent ID 升序，最多取 100 个；不依赖数据库返回顺序。

### 4.3 RuleScore

权威规则分为 100 分制：

```text
RuleScore = TaskMatchScore      // 0..60
          + ReputationScore     // 0..25
          + PriceTimeScore      // 0..10
          + AvailabilityScore   // 0..5
```

各一级维度含义：

- **任务匹配 60 分**：语义、标签、必需能力、分类、语言、工具和交付格式等匹配程度；
- **五维信誉 25 分**：交付质量、响应速度、稳定性、沟通协作、合规守约；
- **价格与时间 10 分**：相对预算、报价竞争力、预计时限和历史时限可靠度；
- **可用性 5 分**：当前健康、剩余容量和近期负载。

`matching-rules-v1` 将任务匹配拆为 dense 25、lexical 15、exact 20；五维信誉各 5 分；正式价格预算余量与截止时间余量各 5 分；剩余容量 5 分。相关性使用 `0..10000` 定点数，所有输入先截断后以整数计算。冷启动先验、信誉时间衰减仍需在后续算法版本中通过数据验证后冻结。

### 4.4 模型有限调整

CTR/LTR 模型只能在规则分基础上调整 `-5..+5`：

```text
ModelDelta  = clamp(modelOutput, -5, +5)
RankingScore = RuleScore + ModelDelta
```

规则分保持权威，模型不可用、未达到上线标准或被关闭时，`ModelDelta = 0`。模型不能让不满足硬条件的 Agent 重新进入候选池。

`matching-rules-v1` 已固定将 `RankingScore` 截断到 `0..100`。

### 4.5 合格池

Agent 必须同时满足：

```text
RuleScore >= 60
RankingScore >= 60
RankingScore >= bestRankingScore - 10
```

然后最多保留 20 个进入 FairShuffle。候选不足 3 个时返回实际数量，不能为了凑满 3 个放宽质量门槛。

“最高分”应在同一任务、同一匹配修订、同一算法版本的已评分集合中计算。合格池截断前必须使用稳定的并列排序规则，例如 `RankingScore DESC, RuleScore DESC, agent_id ASC`，避免数据库无序结果破坏可复现性。

## 5. FairShuffle 洗牌逻辑

### 5.1 目标

FairShuffle 不是普通的随机打乱。它要同时满足：

- 高分 Agent 获得更高曝光概率，但不是永久垄断前三名；
- 低质量 Agent 永远不能借随机性越过质量门槛；
- 一次结果中不能重复 Agent；
- 同一服务商默认最多出现 1 个 Agent；
- 15% 探索只能影响第三位；
- 相同任务、修订和算法版本刷新时结果不变；
- 每次展示可解释、可复现、可审计。

因此它本质上是“质量门槛内的确定性、带权、不放回抽样”。

### 5.2 已冻结的规则

当前规格已经明确：

- 输入只能来自合格池，最多 20 个；
- 最终最多返回 3 个；
- 使用确定性 seed；
- 按权重进行不放回抽样；
- 同一服务商默认最多一个候选；
- 探索流量为 15%，且探索 Agent 只能在第三位；
- `task_id + match_revision + algorithm_version` 相同，输出必须稳定；
- 快照保存权重、概率、位置、探索标记和 seed 摘要。

### 5.3 `fair-shuffle-v1` 已冻结细节

T-202 已将此前待决项冻结为：

- 权重为 `max(1, RankingScore - 60 + 1)`；
- 使用逐位置整数加权、不放回抽取，拒绝采样消除模偏；
- seed 采用版本化 HMAC-SHA256 密钥和长度前缀编码；
- MVP 服务商身份使用 Agent `owner_id` 映射的 `ProviderID`，默认最多 1 个；
- `ExposureCount < 100` 或 `EffectiveSamples < 20` 定义为探索候选；
- 15% 探索从独立探索池抽取且只允许第三位，无候选时回退主池；
- 合格池在洗牌前已按 `RankingScore DESC, RuleScore DESC, agent_id ASC` 截断至 20；
- 所有随机与权重运算使用整数；
- 快照保存每一步条件概率的精确分子/分母，而不是估算边际概率。

修改任何上述参数都必须发布新的 `algorithm_version`，不能静默改变历史结果。企业关联账户合并仍需要后续独立的服务商主体模型。

### 5.4 已实现版本

以下方案已由 `fair-shuffle-v1` 实现。

#### 5.4.1 确定性 seed

```text
seedMaterial = lengthPrefixedEncode(
    task_id,
    task_spec_hash,
    match_revision,
    algorithm_version,
    seed_key_version
)

seed = HMAC-SHA256(versionedShuffleSecret, seedMaterial)
seedDigest = SHA256(seed)
```

使用 HMAC 可以降低 Agent 根据公开输入预测并操纵结果的风险。数据库只需在匹配快照保存 `seedDigest` 和密钥版本，秘密本身不能写入日志或快照。

如果未来要求第三方公开验证公平性，可升级为 commit-reveal 或外部随机信标；直接对公开字段做普通哈希虽然易于复现，但更容易被可控输入博弈。

#### 5.4.2 权重转换

采用简单、可解释、整数化的权重：

```text
weight = max(1, RankingScore - qualificationFloor + 1)
```

当门槛为 60 时，60 分权重为 1，70 分权重为 11。若实测头部过度集中，可在新算法版本中引入温度参数，而不是直接修改旧版本。

#### 5.4.3 带权不放回抽样

实现使用逐位置加权抽取：

```text
for position in [1, 2, 3]:
    eligible = removeSelectedAndConflictingProviders(pool)
    p_i = weight_i / sum(eligibleWeights)
    selected = deterministicWeightedDraw(seed, position, eligible)
```

候选按 Agent ID 稳定排序；每个位置以 HMAC 域标签派生 64 位整数并用拒绝采样消除模偏，因而可以精确保存当步概率且不存在跨平台浮点差异。

#### 5.4.4 服务商上限

每选中一个 Agent 后，从后续位置候选中排除相同 provider identity 的其他 Agent。MVP 以 `owner_id` 映射的 `ProviderID` 作为 provider identity；若存在企业多账户或关联钱包，仍必须升级为独立的服务商主体 ID，不能只靠钱包地址去重。

#### 5.4.5 15% 探索位

使用 seed 派生稳定的探索判定：

```text
explore = deterministicUnit(seed, "explore") < 0.15
```

- 第一、第二位始终从主合格池按权重抽样；
- 仅当 `explore = true` 且存在合格探索候选时，第三位从探索子池抽样；
- 探索子池仍必须满足全部硬过滤、双 60 分门槛、最高分差 10 分和服务商限制；
- 没有可用探索候选时，第三位回退到主合格池；
- 探索候选固定为曝光量低于 100 或有效样本量低于 20 的合格 Agent，不以低分作为探索资格。

### 5.5 建议伪代码

```text
function recommend(task, revision, policy):
    assert task.status is matchable

    eligible, excluded = hardFilter(task, agents)

    semantic = semanticRecall(task, eligible, top=100)
    lexical = lexicalRecall(task, eligible, top=100)
    exact = exactRecall(task, eligible, top=100)
    recalled = fuseAndDeduplicate(semantic, lexical, exact, limit=100)

    scored = []
    for agent in recalled:
        ruleBreakdown = calculateRuleScore(task, agent, policy)
        modelDelta = boundedModelAdjustment(task, agent, policy) // -5..+5 or 0
        rankingScore = ruleBreakdown.total + modelDelta
        scored.append(agent, ruleBreakdown, modelDelta, rankingScore)

    best = max(scored.rankingScore)
    pool = scored.filter(
        ruleScore >= 60
        and rankingScore >= 60
        and rankingScore >= best - 10
    )
    pool = stableSort(pool).take(20)

    seed = deriveSeed(task.id, task.specHash, revision, policy.algorithmVersion)
    selected = deterministicFairShuffle(pool, seed, max=3, providerCap=1)

    persistImmutableSnapshot(task, revision, policy, scored, excluded, selected)
    return selected
```

## 6. 匹配修订与刷新稳定性

稳定性的业务语义应当是：

- 浏览器刷新、BFF 重试、Worker 重投不增加 `match_revision`；
- 相同 `task_id + task_spec_hash + match_revision + algorithm_version` 必须复用已有快照；
- 用户修改任务规格、预算、时限或其他有效匹配输入后，创建新修订；
- Agent 健康或容量变化是否触发新修订，需要明确策略，不能在读取页面时隐式重新洗牌；
- 新算法先影子运行，正式切换时增加 `algorithm_version`；
- 历史快照永久保留可读，旧概览在修订变化后标记为 `OBSOLETE`。

推荐接口应优先返回已持久化快照，而不是每次 GET 请求即时重算。这既保证刷新稳定，也避免用户通过反复刷新“抽卡”。

## 7. 快照与可解释性

建议至少保存以下信息：

### 7.1 匹配修订

- `task_id`、`task_spec_hash`、`match_revision`；
- `algorithm_version`、规则版本、模型版本；
- seed 摘要、策略参数摘要；
- 创建时间、完成状态和降级模式。

### 7.2 每个被评估 Agent

- Agent、服务商和价格版本；
- 三路召回来源及各通道名次；
- 硬过滤结果和排除 reason codes；
- RuleScore 四个一级维度及更细分项；
- ModelDelta 和 RankingScore；
- 合格池资格及未入池原因；
- 抽样权重、当步条件概率、随机 key；
- 最终位置和探索标记。

用户侧解释应展示稳定、可理解的事实，例如“必需能力完全匹配”“报价在预算内”“健康检查有效”“近 90 天按时完成率”。内部模型特征、秘密 seed 和可能泄露风控规则的细节不应直接返回浏览器。

## 8. 降级逻辑

推荐依赖失败时应降级能力，而不是降低质量门槛：

| 故障 | 建议降级 | 不允许的行为 |
| --- | --- | --- |
| Qdrant/Embedding 不可用 | 使用关键词和精确召回 | 跳过硬过滤或随机补足候选 |
| CTR/LTR 不可用 | `ModelDelta = 0` | 使用缓存中的未知版本模型分 |
| Redis 不可用 | 从 PostgreSQL 快照读取或同步执行一次受控计算 | 把 Redis 当作唯一快照真相源 |
| 单路召回超时 | 记录降级并使用其余通道 | 悄悄改变算法版本或放宽 60 分门槛 |
| 合格候选不足 3 个 | 返回实际数量 | 用不合格 Agent 凑满 3 个 |

降级模式必须进入匹配快照、指标、日志和审计事件。

## 9. 数据与代码现状差距

| 能力 | 当前状态 | 主要缺口 |
| --- | --- | --- |
| Agent 基础字段 | 部分完成 | 已有分类、标签、能力、语言、时限、健康、容量、价格和所有者 |
| 硬过滤 | 领域层已实现 | 生产候选查询仍需接入审核、风控、向量版本等权威字段 |
| 三路召回 | adapter 与本地通道已实现 | dense 接口已留出；Qdrant/Embedding 生产 adapter 和索引待接入 |
| RuleScore | `matching-rules-v1` 已实现 | 冷启动先验和信誉衰减需后续数据验证 |
| 五维信誉 | UI mock | 无权威信誉表、时间窗口和更新流水 |
| 模型调整 | 接口与 ±5 限幅已实现 | 无训练/推理 adapter、模型门禁和版本记录；当前可固定回退为 0 |
| 合格池 | 领域规则已实现 | 生产候选查询仍需接入权威信誉和风控数据 |
| FairShuffle | `fair-shuffle-v1` 已实现 | 企业关联账户仍需统一服务商主体模型 |
| 推荐 UI | 原型可用 | 当前使用静态数据和浏览器本地排序 |
| Agent 执行协议 | `agent-execution-v1` 已实现 | 已覆盖 HTTPS 调用、签名回调、逻辑/网络尝试分层、成本停止与 fencing；生产组装由概览编排接入 |
| 审计与可复现 | 匹配快照与执行回调证据已实现 | 概览客观校验、allocation 结算与选择审计由后续任务补齐 |

## 10. 实施建议

建议按以下顺序落地：

1. T-201、T-202 已冻结并实现规则评分、融合、FairShuffle、revision 和不可变快照。
2. Engine `internal/matching` 已保持过滤、召回、评分、调整、洗牌和仓储接口分离。
3. PostgreSQL 已原子保存匹配修订、完整候选和展示快照；Redis 只可做缓存或协调。
4. T-203 已实现 `agent-execution-v1`、签名回调、成本上限、逻辑执行/网络尝试分离与 PostgreSQL fencing 租约语义。
5. 下一步由 T-204 把当前匹配快照、脱敏概览简报、独立 allocation、统一截止时间和客观结果校验接入执行协议。
6. BFF 只聚合并脱敏 Engine 快照；删除 Web 中的权威评分判断。
7. 模型调整以 feature flag 单独上线，先影子运行，再依据离线指标、公平性护栏和回滚阈值启用。

## 11. 必须覆盖的测试

- 每一个硬过滤条件的正反例和组合边界；
- 三路召回去重、通道失败和超过 100 个时的稳定截断；
- RuleScore 分项总和、缺失数据、边界值和固定精度；
- 模型调整永远位于 `-5..+5`，模型故障时严格归零；
- 双 60 分门槛、最高分差 10 分、最多 20 个；
- 相同输入、不同进程和不同机器产生相同候选与顺序；
- 带权抽样不重复，同服务商最多一个；
- 探索长期频率接近 15%，且只出现在第三位；
- 探索 Agent 始终满足质量门槛；
- 候选不足 3 个时不补低质量 Agent；
- 修改有效输入增加修订，普通刷新和重试不增加修订；
- 快照、分项分数、排除原因、概率和 seed 摘要完整可审计；
- 发布者与自有/关联 Agent 永远不能互相匹配。

除示例测试外，应增加属性测试和跨进程 golden tests，防止 PRNG、排序或数值实现升级后悄悄改变同一算法版本的结果。

## 12. 相关文件

- 产品需求：`docs/AI-Agent平台-需求文档.md` 第 7.4–7.6 节；
- 匹配需求：`spec/2.matching-overview-selection/requirements.md`；
- 匹配设计：`spec/2.matching-overview-selection/design.md`；
- 实施任务：`spec/2.matching-overview-selection/tasks.md`；
- 当前 UI 排序：`apps/web/src/app/pages/publisher/AgentRecommendations.tsx`；
- 当前 mock 候选：`apps/web/src/app/lib/mock.ts`；
- Agent 数据基础：`services/engine/internal/persistence/postgres/migrations/000003_agents.up.sql`。
