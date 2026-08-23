# Agent Runtime

`@agent-platform/agent-runtime` 是业务 Agent 的公共运行底座，不包含任何具体领域提示词或报告结构。

统一提供：

- 有并发上限的异步任务队列及 `queued/running/completed/failed/canceled` 生命周期。
- 基于 UUID 的本地 JSON 任务仓库和原子写入。
- 持久化记录带版本号，并可通过 `legacyResultFields` 兼容旧 Agent 的结果字段。
- 可配置路径的 HTTP API、健康检查、Bearer Token、请求体上限和取消接口。
- 可供协议型 Agent 复用的常量时间 Bearer 校验、有界 JSON 读取与 JSON 响应工具。
- 公网 HTTPS URL 校验、安全重定向、响应大小及内容类型限制。
- OpenAI-compatible `/chat/completions` JSON 模型适配器，支持可选严格 JSON Schema 与响应模型元数据回调。

业务 Agent 只需实现 `JobExecutor<TRequest, TResult>`、请求校验和可选的文本渲染器。

平台型 Agent 还可以使用 `AgentExecutionAdapter` 和 `createAgentExecutionServer` 实现统一的
`agent-execution-v1`。公共适配层负责 Envelope 校验、输入哈希、公网引用限制、幂等与 fencing、
取消信号、JSON 产物、Bearer 鉴权以及 HMAC 回调；业务 Agent 只实现输入解析、概览和正式执行。

```ts
import {
  AsyncJobService,
  FileJobRepository,
  createAgentHttpServer,
  type JobExecutor,
} from "@agent-platform/agent-runtime";

const executor: JobExecutor<MyRequest, MyResult> = {
  execute: async (request, context) => runDomainAgent(request, context.signal),
};

const service = new AsyncJobService(executor, new FileJobRepository(".data"));
const server = createAgentHttpServer({
  manifest: { id: "my-agent", version: "0.1.0" },
  service,
  basePath: "/v1/tasks",
  parseRequest: (value) => mySchema.parse(value),
});
```

默认 API：

- `GET /health`
- `POST <basePath>/jobs`
- `GET <basePath>/jobs/:id`
- `GET <basePath>/jobs/:id/<resultPath>`
- `POST <basePath>/jobs/:id/cancel`
