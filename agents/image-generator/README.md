# 一句话图片生成 Agent

基于 Mastra 与智谱 GLM-Image。用户提交一句自然语言需求，Agent 自动补足必要的视觉细节、调用图片接口并立即转存结果。

## 启动

```bash
export ZAI_API_KEY="..."
pnpm --filter @agent-platform/image-generator-agent dev
```

可选环境变量：

- `IMAGE_AGENT_PORT`：监听端口，默认 `8092`
- `IMAGE_AGENT_DATA_DIR`：任务与图片目录，默认 `.data/image-generator`
- `IMAGE_AGENT_API_TOKEN`：Bearer Token；生产环境必填且至少 24 字符
- `IMAGE_AGENT_PUBLIC_BASE_URL`：图片公网 HTTPS 基地址；生产环境必填
- `ZAI_BASE_URL`：智谱 API 地址，默认 `https://open.bigmodel.cn/api/paas/v4`
- `IMAGE_AGENT_PLANNER_MODEL`：负责理解需求并调用工具的模型，默认 `glm-4.5-flash`
- `IMAGE_AGENT_JOB_CONCURRENCY`：并行生成数，默认 `1`

## 调用

```bash
curl -X POST http://localhost:8092/v1/image-generation/jobs \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer YOUR_TOKEN' \
  -d '{"prompt":"一只在月球咖啡馆喝拿铁的橘猫，复古科幻海报风格"}'
```

响应中的 `statusUrl` 用于查询任务状态；完成后读取其 `resultUrl`，结果内的 `imageUrl` 即图片地址。默认尺寸为 `1280x1280`、质量为 GLM-Image 唯一支持的 `hd`；还支持 `1568x1056`、`1056x1568`、`1472x1088`、`1088x1472`、`1728x960`、`960x1728`。

同一个服务还提供平台统一的 `agent-execution-v1` 接口：`POST /v1/executions`、
`/status`、`/cancel` 和 `/deliverable`。`GET /health` 返回 `protocolVersion: "1"`，
结构化交付物位于 `/v1/artifacts/:id`。生产环境还必须配置
`IMAGE_AGENT_CALLBACK_KEY_BASE64`（至少 32 字节）及其对应的 Engine 回调验签密钥版本。
