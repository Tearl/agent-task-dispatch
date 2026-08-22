import type { DeliverableArtifact, ReadingArtifact } from "../domain/types.ts";

export const SAFETY_NOTICE = "本解读提供象征性的关系反思视角，不用于证明他人的真实想法，也不能保证未来事件发生。";

export function renderMarkdown(artifact: Omit<ReadingArtifact, "markdown">): string {
  const cards = artifact.cards.map((card, index) => [
    `### ${index + 1}. ${card.position}：${card.name}${card.orientation === "upright" ? "（正位）" : "（逆位）"}`,
    "",
    `基础牌义：${card.baseMeaning}`,
    "",
    escapeText(card.contextualInterpretation),
  ].join("\n")).join("\n\n");

  return [
    "# 关系镜像塔罗解读",
    "",
    `问题：${escapeText(artifact.questionSummary)}`,
    "",
    cards,
    "",
    "## 综合解读",
    "",
    escapeText(artifact.synthesis),
    "",
    section("关系互动线索", artifact.relationshipDynamics),
    section("你可以控制的部分", artifact.controllableFactors),
    section("你无法控制的部分", artifact.uncontrollableFactors),
    section("行动建议", artifact.actionSuggestions),
    section("反思问题", artifact.reflectionQuestions),
    "## 不确定性",
    "",
    escapeText(artifact.uncertainty),
    "",
    `> ${artifact.safetyNotice}`,
  ].join("\n");
}

export function renderSafetyMarkdown(artifact: Omit<Extract<DeliverableArtifact, { kind: "safety_redirect" | "declined" }>, "markdown">): string {
  return [
    artifact.kind === "safety_redirect" ? "# 先处理现实中的安全" : "# 这个请求无法通过塔罗处理",
    "",
    artifact.message,
    "",
    section("建议下一步", artifact.nextSteps),
    `> ${artifact.safetyNotice}`,
  ].join("\n");
}

function section(title: string, values: string[]): string {
  return [`## ${title}`, "", ...values.map((value) => `- ${escapeText(value)}`), ""].join("\n");
}

function escapeText(value: string): string {
  return value
    .replace(/&/gu, "&amp;")
    .replace(/</gu, "&lt;")
    .replace(/>/gu, "&gt;")
    .replace(/([\\`*_[\]()#|])/gu, "\\$1");
}
