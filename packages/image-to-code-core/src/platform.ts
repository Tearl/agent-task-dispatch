import type { PlatformExecutor, OverviewResult } from "@agent-platform/agent-runtime";
import { parseImageToCodeExecutionInput, type ImageToCodeExecutionInput } from "./input.ts";
import type { GeneratedProject } from "./schema.ts";

export function createImageToCodePlatformExecutor(
  generate: (input: ImageToCodeExecutionInput, signal?: AbortSignal) => Promise<GeneratedProject>,
): PlatformExecutor<ImageToCodeExecutionInput, GeneratedProject> {
  return {
    parseInput: parseImageToCodeExecutionInput,
    execute: (input, context) => generate(input, context.signal),
    overview: (input) => imageToCodeOverview(input),
  };
}

function imageToCodeOverview(input: ImageToCodeExecutionInput): OverviewResult {
  const target = input.target?.trim() || "React + TypeScript + CSS";
  return {
    schemaVersion: "overview-result-v1",
    understandingSummary: `根据提供的界面截图重建一个可运行的 ${target} 前端项目。`,
    approach: ["分析截图的布局、视觉层级和组件边界", `使用 ${target} 实现响应式界面`, "校验生成文件路径与项目完整性"],
    deliverableStructure: ["完整项目源码", "依赖与构建配置", "运行说明、假设与已知限制"],
    keyRisks: ["截图无法表达完整交互与所有响应式状态", "字体、品牌素材和隐藏状态可能需要合理近似"],
    estimatedDurationSeconds: 360,
    ...(input.prompt?.trim() ? { sample: input.prompt.trim().slice(0, 4_000) } : {}),
  };
}
