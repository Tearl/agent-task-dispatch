# Tarot Relationship Agent

关系反思型塔罗 Agent MVP。它使用固定三牌“关系镜像”牌阵，帮助成年用户区分可观察事实、个人感受和象征性假设，并提供尊重双方边界的行动建议。

本 Agent 不把塔罗作为事实来源，不声称读取第三方真实想法，不保证复合、分手、出轨或其他未来结果。

通用的模型调用、公网 URL 防护、Bearer 鉴权和有界 JSON HTTP 工具由 `@agent-platform/agent-runtime` 提供；平台执行协议与塔罗领域状态仍由本 Agent 自己维护。

## MVP 能力

- 完整 78 张 Rider–Waite–Smith 语义牌组，支持正逆位。
- 三个固定位置：当前关系能量、核心互动模式或阻碍、用户可采取的行动。
- 基于 `taskSpecHash + assignmentId` 的确定性洗牌证明。
- 同一 assignment 的 V1–V3 保留同一组牌；反馈只深化原解读。
- 输出结构化 JSON，并内嵌可展示的 Markdown。
- 支持 `gentle`、`direct`、`neutral` 三种语气。
- 对自伤、暴力、监控、操控和未成年人敏感关系进行安全转介或拒绝。
- 可选 OpenAI-compatible JSON 模型；未配置或模型输出越界时使用本地模板降级。

## 平台协议

健康检查：

```http
GET /health
```

返回：

```json
{"status":"healthy","protocolVersion":"1","agent":"tarot-relationship","version":"0.1.0"}
```

Agent 实现 `agent-execution-v1` 的四个端点：

- `POST /v1/executions`
- `POST /v1/executions/status`
- `POST /v1/executions/cancel`
- `POST /v1/executions/deliverable`

请求必须绑定 `X-Agent-Protocol-Version`、`Idempotency-Key` 和执行 Envelope。配置 `TAROT_AGENT_API_TOKEN` 后，还必须携带 `Authorization: Bearer <token>`。

产物可以通过 `GET /v1/artifacts/:artifactId` 读取，并使用相同 Bearer Token 保护。生产环境必须配置 `TAROT_AGENT_PUBLIC_BASE_URL`，使 `deliverableRef` 成为 Engine 可访问的 HTTPS URL。

输入引用支持：

- 生产：返回 `application/json` 的公网 HTTPS 短期引用。
- 本地测试：`data:application/json;base64,...`。

Agent 会在处理前重新计算 `inputHash`，不接受重定向、非 JSON、超过 128 KiB 或解析到受限网络的 HTTPS 输入。

## 输入

参见 [`examples/request.json`](./examples/request.json)：

```json
{
  "relationshipStage": "dating",
  "question": "最近沟通越来越少，我应该主动联系吗？",
  "context": "双方交往半年，最近两周联系频率下降。",
  "tone": "gentle",
  "drawMode": "platform_random",
  "ageConfirmed": true
}
```

正式 V2/V3 可以增加 `feedback`，但不能通过反馈要求重新抽牌。新问题或重新抽牌应创建新任务或变更单。

## 启动

```bash
cp agents/tarot-relationship/.env.example agents/tarot-relationship/.env
pnpm --filter @agent-platform/tarot-relationship-agent dev
```

默认监听 `8091`。不设置 `LLM_MODEL` 时可以离线运行本地模板解读。

生产模式额外强制要求：

- 至少 24 字符的 API Token。
- HTTPS `TAROT_AGENT_PUBLIC_BASE_URL`。
- 至少 32 字节的回调 HMAC Key。

回调 JSON 使用 `TAROT_AGENT_CALLBACK_KEY_BASE64` 进行 HMAC-SHA256 签名，并通过 `X-Agent-Signature` 发送给 Engine。

## 版本与抽牌规则

- 正式抽牌作用域是 `assignmentId`，不是逻辑执行 ID。
- V1、V2、V3 的反馈和输入哈希可以变化，但只要 assignment 不变，牌和正逆位就不变。
- 重试相同逻辑执行会复用已有状态与产物，不产生第二份内容。
- 抽牌证明包含算法、牌组版本、牌阵版本、作用域 ID 和种子摘要。
- 当前随机算法为 `sha256-counter-fisher-yates-v1`。

## 安全边界

- 不联网搜索用户或第三方，不访问社交账号。
- 不提供跟踪、定位、偷看设备、操控、报复等建议。
- 不提供医疗、法律或财务结论。
- 识别到自伤、他伤或关系暴力时，不生成牌面判断，优先返回现实安全建议。
- 输入完成后会从内存记录中清除内联正文和一次性回调 nonce；产物文件权限为当前用户读写。

## 校验

```bash
pnpm --filter @agent-platform/tarot-relationship-agent check-types
pnpm --filter @agent-platform/tarot-relationship-agent test
pnpm --filter @agent-platform/tarot-relationship-agent build
```

测试覆盖牌组完整性、确定性抽牌、V1–V3 不换牌、安全分类、模型越界降级、概览限制、输入哈希、执行幂等和协议绑定。

## 当前 MVP 限制

- 执行状态保存在进程内存中，最终产物持久化到数据目录；进程重启后的执行恢复属于下一阶段。
- 尚未提供牌面图片，避免在未冻结授权策略前引入图像版权问题。
- HTTP 请求鉴权目前采用 Bearer Token；若 Engine 后续冻结请求 HMAC 规范，应替换为统一 Agent SDK 验证器。
