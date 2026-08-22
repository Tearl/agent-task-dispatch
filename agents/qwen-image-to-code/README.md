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
