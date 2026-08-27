# AI Agent Platform

AI Agent 任务交易与分发平台 MVP。项目由 [Create Better-T-Stack](https://www.better-t-stack.dev/) 创建 monorepo 基座，并按项目架构补充 Next.js BFF、Go 核心分发引擎、Ethereum 托管合约和 AWS 本地模拟环境。

## Technology stack

- C 端 UI：React 19、Vite、React Router、Tailwind CSS
- SSR / BFF：Next.js 16，目标部署 AWS Lambda
- 核心引擎：Go，目标部署 AWS ECS 或 EKS
- AI 编排规划：LangGraph（托管确认后判断单 Agent 或多 Agent DAG）
- 消息：Amazon SNS、SQS、DLQ
- 数据：PostgreSQL、Redis；Milvus/Pinecone 延后到 V1
- 合约：Solidity、Foundry、Ethereum 测试链
- 边缘与云：Cloudflare CDN/WAF、AWS
- Monorepo：Nx、pnpm workspace

## Repository layout

```text
apps/
  web/                 React/Vite C 端应用
  bff/                 Next.js SSR 与 BFF
agents/
  competitor-intelligence/  独立竞品情报 Agent
  tarot-relationship/       独立关系塔罗 Agent
  image-generator/          Mastra 一句话图片生成 Agent
  qwen-ui-prototype/        Qwen-Image 软件 UI 高保真原型 Agent
  seedream-visual-design/   Seedream 软件品牌视觉设计 Agent
  viral-video-analyzer/     独立短视频爆款分析 Agent
contracts/
  escrow/              任务资金托管合约
services/
  engine/              Go 状态机、分发与调度引擎
packages/
  agent-runtime/       业务 Agent 的任务、HTTP、模型与抓取公共底座
  ui/                  共享 UI 组件与 Tailwind 主题
  env/                 TypeScript 环境变量定义
  config/              TypeScript 共享配置
infrastructure/
  localstack/          本地 SNS/SQS/DLQ 初始化
docs/
  architecture.md      架构边界
```

## Prerequisites

- Node.js（项目使用 pnpm 11）
- Go 1.26+
- Foundry
- Docker Desktop / Docker Engine

## Start locally

```bash
cp .env.example .env
# Generate two distinct ephemeral local-development keys. Persist replacements
# in your local secret manager or .env if they must survive shell restarts.
export AGENT_CREDENTIAL_KEK_BASE64="$(node -p "require('node:crypto').randomBytes(32).toString('base64')")"
export AGENT_CREDENTIAL_IDEMPOTENCY_HMAC_BASE64="$(node -p "require('node:crypto').randomBytes(32).toString('base64')")"
pnpm install
pnpm infra:up
pnpm dev
```

### Local escrow chain

Task matching is deliberately unavailable until the published task has a
canonical escrow deposit. Run a local Anvil chain in a separate terminal:

```bash
pnpm dev:chain
```

Export the Engine's local-only selection signing key through your shell or
secret manager, derive its public address, and deploy the contract:

```bash
export SELECTION_PROOF_SIGNING_KEY_HEX="<local-only-key>"
export SELECTION_PROOF_SIGNER_ADDRESS="$(cast wallet address "$SELECTION_PROOF_SIGNING_KEY_HEX")"
pnpm chain:deploy
```

The deployment writes non-secret RPC, contract, resolver, start-block, and
confirmation settings to ignored `.env.chain`. Load that file into the Engine
process together with the signing key. Never write the signing key to `.env`,
`.env.chain`, logs, or command output.

After task publication the Web flow creates an immutable funding intent. The
publisher wallet calls `TaskEscrow.createTask`; only a confirmed `TaskCreated`
event with the exact chain task ID, publisher, contract, and amount moves the
task from `pending_escrow` to `escrowed`. A reorg reverses the funding journals
and returns the task to `pending_escrow`.

Agent onboarding now stores an encrypted `agent-execution-v1` protocol bundle:
the Engine-to-Agent bearer token, a distinct 32-byte callback HMAC key, and its
version. The plaintext exists only during the protected request and in process
memory. Engine startup restores current bundles from immutable ciphertext, so
new Agents no longer require a manual `ENGINE_AGENT_RUNTIME_CREDENTIALS_JSON`
restart cycle.

Never reuse either development key in another environment. Production must inject two independently managed 32-byte keys and a versioned `AGENT_CREDENTIAL_KEY_REF` through the environment secret manager; the production KMS vendor remains an open decision.

Local endpoints:

- React customer app: <http://localhost:5173>
- Next.js BFF and SSR: <http://localhost:3000>
- Go Engine: <http://localhost:8080>
- LangGraph Orchestrator: <http://localhost:8090>
- BFF health: <http://localhost:3000/api/health>
- LocalStack: <http://localhost:4566>

`pnpm --filter web start` serves the production React Router build and proxies
same-origin `/api/*` requests to the server-only `BFF_URL` origin (default
`http://localhost:3000`). Keep browser code on relative `/api` URLs so session
cookies remain first-party; `VITE_BFF_URL` is used only by the Vite development
proxy.

Agent onboarding stores a clean HTTPS protocol base URL. Engine probes its
`/health` endpoint with a three-second timeout and expects a bounded JSON response:
`{"status":"healthy","protocolVersion":"1"}`. Redirects and private-network
targets are rejected by default; `AGENT_HEALTH_ALLOW_PRIVATE_NETWORKS=true` is
for explicit local testing only and must remain disabled in production.

### Matching, overview, and execution APIs

The browser calls the same-origin BFF only. The BFF forwards these publisher-
owned workflow APIs to Engine:

- `POST /v1/tasks/{taskId}/matching-runs` creates or replays a sealed matching
  revision from the current immutable task spec, active Agent records, live
  capacity, health, and frozen prices.
- `POST /v1/tasks/{taskId}/overview-batches` authorizes discovery funds and
  dispatches one overview execution for each selected snapshot slot.
- `POST /v1/tasks/{taskId}/overview-batches/{batchId}/slots/{slotId}/finalize`
  validates a terminal artifact and settles or releases its allocation.
- `GET /v1/tasks/{taskId}/executions` returns a sanitized execution projection;
  it never returns input references, callback nonces, or transport credentials.

Workflow commands advance the task aggregate through `matching`,
`overview_generating`, and `awaiting_selection` under a database row lock. Each
transition increments the aggregate version and appends domain and audit events;
draft, unfunded, assigned, and terminal tasks cannot enter the workflow.

`EXECUTION_INPUT_BASE_URL` is the public HTTPS Engine origin used in non-secret
input references. Agents authenticate those reads with their existing runtime
bearer credential. Engine verifies that the credential is bound to the exact
Agent, task input reference, execution stage, and deadline before regenerating
the immutable, specification-bound input. Only Agent-contract allowlisted fields
are emitted and common credential or identity patterns in text are redacted.
Artifact downloads use the same credential and are pinned
to the registered Agent endpoint origin to prevent credential forwarding.

The main publisher and Agent workspaces now read task, catalog, runtime, finance,
execution, and audit-event projections through `/v1/workspace/*`; the former
sample records are no longer imported by those pages.

### Engine Outbox worker

Set `ENGINE_WORKER_ENABLED=true` only after configuring the versioned matching
and execution secrets in `.env.example`. `ENGINE_AGENT_RUNTIME_CREDENTIALS_JSON`
is a server-only JSON object keyed by Agent ID; its bearer token authorizes
Engine-to-Agent calls and its base64 callback key verifies Agent callbacks.
These values stay in process memory and are never written to PostgreSQL or logs.

The worker claims only registered command topics with PostgreSQL leases and
`FOR UPDATE SKIP LOCKED`. Formal execution commands are retried with bounded
exponential backoff and move to dead-letter state after the configured attempt
limit or a permanent validation failure. Other Outbox topics remain pending for
their dedicated publishers/consumers.

### Independent SNS/SQS Outbox publisher

Run the publisher as a separate process from the Engine API:

```bash
pnpm --filter @agent-platform/engine dev:publisher
```

It claims only its configured topics with the same fenced PostgreSQL leases.
`task-events` and `agent-events` publish to separate SNS topics;
`admin.operation.requested` publishes to a dedicated SQS FIFO queue. Each AWS
message contains an `outbox-envelope-v1` body plus `message_id`, `dedupe_key`,
`topic`, and `version` message attributes. The Outbox row is completed only
after AWS returns a broker message ID.

Delivery is intentionally at least once: a process can stop after AWS accepts a
message but before PostgreSQL records `published_at`. Every downstream consumer
must therefore record `(consumer_name, message_id)` in an idempotency ledger.
Consumers whose domain effect is a single PostgreSQL transaction must write the
ledger row in that transaction. Workflows that cross an external boundary must
additionally reuse stable domain idempotency and fencing identities across a
crash window. SQS FIFO deduplication reduces short-window duplicates but does
not replace either guarantee.

`ENGINE_FORMAL_EXECUTION_TRANSPORT=database` leaves formal commands on the
embedded Engine Worker and prevents the independent publisher from claiming
them. Setting it to `sqs` makes the publisher route
`agent.execution.formal.requested` to `TASK_EXECUTION_QUEUE_URL`; the API keeps
the signed Agent callback endpoint active but refuses to start its database
Worker. This prevents both transports from consuming the same topic.

Run the independent formal-execution consumer with the same execution secrets
as the API:

```bash
pnpm --filter @agent-platform/engine dev:consumer
```

The consumer long-polls one FIFO message at a time, strictly binds the four SQS
message attributes to `outbox-envelope-v1`, and renews visibility while the
authoritative Engine handler is running. A failed handler changes visibility
with bounded exponential backoff; the queue redrive policy moves repeated poison
messages to its DLQ. A successful handler inserts immutable
`processed_messages(consumer_name, message_id)` evidence before deleting the SQS
message. Redelivery after that point skips the handler. A stop between the
business effect and ledger insert is also safe because formal execution creation,
network attempts, Agent calls, and delivery transitions reuse their existing
logical idempotency and fencing identities.

LocalStack provisions separate task/Agent event topics, projection queues,
formal/admin FIFO command queues, and one DLQ per queue. In production, omit
`AWS_ENDPOINT_URL` and grant the publisher ECS Task Role only `sns:Publish` on
the two topics and `sqs:SendMessage` on the command queues. Grant the consumer
role only `sqs:ReceiveMessage`, `sqs:DeleteMessage`, and
`sqs:ChangeMessageVisibility` on the formal queue. Do not configure long-lived
AWS keys in production task definitions.

## Quality commands

```bash
pnpm check-types
pnpm test
pnpm build
```

## Scaffold command

The reproducible Create Better-T-Stack command used for the base is:

```bash
pnpm create better-t-stack@latest agent-platform \
  --frontend react-router \
  --backend none \
  --runtime none \
  --database none \
  --orm none \
  --api none \
  --auth none \
  --payments none \
  --addons nx \
  --examples none \
  --db-setup none \
  --web-deploy none \
  --server-deploy none \
  --no-git \
  --package-manager pnpm \
  --no-install
```

Create Better-T-Stack supports one web framework per generation and does not scaffold Go. The React/Vite application was generated by the CLI; the Next.js BFF and Go Engine were then added as first-class workspace projects without moving core business rules into Node.js.

## Open decisions

- Ethereum testnet and escrow asset
- ECS or EKS
- Milvus or Pinecone for V1
- refund, timeout, dispute, and contract administration rules
- production Next.js-to-Lambda deployment adapter
- production credential KMS vendor and key lifecycle
