# Qwen_image-to-code

独立的 Mastra 图片转代码 Agent，使用 `Qwen3.7 Plus` 直接分析截图并返回结构化前端项目文件。

```bash
cp agents/qwen-image-to-code/.env.example agents/qwen-image-to-code/.env
pnpm --filter @agent-platform/qwen-image-to-code-agent dev
```

CLI：

```bash
pnpm --filter @agent-platform/qwen-image-to-code-agent generate -- ./screen.png \
  --target="React + TypeScript + Tailwind CSS" \
  --out=./generated/qwen
```

省略 `--out` 时，CLI 会把完整 JSON 输出到终端；指定后会把 `files` 写成可运行的项目目录。相对路径以 pnpm workspace 根目录为基准。

Mastra API：`POST /api/agents/qwenImageToCodeAgent/generate`。

## 平台协议

使用独立的 `agent-execution-v1` 服务接入 Engine：

```bash
pnpm --filter @agent-platform/qwen-image-to-code-agent dev:platform
```

默认监听 `8094`，提供 `GET /health`、四个 `/v1/executions*` 端点和受 Bearer Token 保护的
`GET /v1/artifacts/:id`。平台输入 JSON 为 `{ image: { data, filename, mediaType }, target?, prompt? }`；
其中 `image.data` 是经过签名校验且不超过 10 MiB 的 base64 图片。生产环境必须配置 API Token、
公网 HTTPS 基地址和至少 32 字节的回调 HMAC Key，变量名见 `.env.example`。
