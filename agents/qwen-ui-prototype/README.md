# Qwen UI Prototype Agent

面向前端开发的高保真 UI/UX 设计稿 Agent。它使用阿里云百炼
`qwen-image-3.0-pro`，强化页面栅格、组件层级以及中英文文字可读性，并在厂商
临时 URL 失效前立即将 PNG 转存到 Agent 的持久化目录。

## 本地启动

```bash
export QWEN_UI_DASHSCOPE_API_KEY="..."
export DASHSCOPE_IMAGE_BASE_URL="https://YOUR_WORKSPACE_ID.cn-beijing.maas.aliyuncs.com"
pnpm --filter @agent-platform/qwen-ui-prototype-agent dev
```

默认端口为 `8095`，Job API 为 `POST /v1/ui-prototypes/jobs`，同时支持平台统一的
`agent-execution-v1` 接口。生产环境必须配置独立的 `QWEN_UI_AGENT_API_TOKEN`、
`QWEN_UI_AGENT_PUBLIC_BASE_URL` 和 32 字节的 `QWEN_UI_AGENT_CALLBACK_KEY_BASE64`。
