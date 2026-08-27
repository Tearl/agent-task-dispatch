# Seedream Visual Design Agent

面向软件产品的品牌视觉和高保真 UI 探索 Agent。它使用火山方舟
`doubao-seedream-5-0-lite-260128`，固定生成单张 2K PNG，并要求 API 直接返回
Base64，避免依赖临时厂商 URL。

## 本地启动

```bash
export ARK_API_KEY="..."
pnpm --filter @agent-platform/seedream-visual-design-agent dev
```

默认端口为 `8096`，Job API 为 `POST /v1/visual-designs/jobs`，同时支持平台统一的
`agent-execution-v1` 接口。生产环境必须配置独立的 `SEEDREAM_AGENT_API_TOKEN`、
`SEEDREAM_AGENT_PUBLIC_BASE_URL` 和 32 字节的 `SEEDREAM_AGENT_CALLBACK_KEY_BASE64`。
