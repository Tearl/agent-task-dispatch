import { readingRequestSchema } from "../domain/schema.ts";
import type { DeliverableArtifact, ReadingArtifact, ReadingRequest, SafetyArtifact } from "../domain/types.ts";
import type { RelationshipInterpreter } from "../interpretation/interpreter.ts";
import { renderMarkdown, renderSafetyMarkdown, SAFETY_NOTICE } from "../interpretation/report.ts";
import { drawRelationshipSpread } from "../randomness/deterministic-draw.ts";
import { classifySafety } from "../security/safety.ts";

export interface ReadingExecutionInput {
  taskSpecHash: string;
  stage: "overview" | "formal";
  scopeId: string;
  formalVersion?: number;
  body: unknown;
  now?: Date;
}

export class TarotReadingService {
  private readonly interpreter: RelationshipInterpreter;

  constructor(interpreter: RelationshipInterpreter) {
    this.interpreter = interpreter;
  }

  async execute(input: ReadingExecutionInput): Promise<unknown> {
    const request = readingRequestSchema.parse(input.body) as ReadingRequest;
    if (input.stage === "overview") return overviewFor(request);

    const decision = classifySafety(request);
    const now = (input.now ?? new Date()).toISOString();
    if (decision.kind !== "normal") return safetyArtifact(decision.kind, decision.reasonCode ?? "unsupported_request", now);

    const formalVersion = input.formalVersion ?? 1;
    const { cards, proof } = drawRelationshipSpread({
      taskSpecHash: input.taskSpecHash,
      scope: "assignment",
      scopeId: input.scopeId,
    });
    const interpretation = await this.interpreter.interpret({ request, cards, formalVersion });
    const withoutMarkdown: Omit<ReadingArtifact, "markdown"> = {
      schemaVersion: "tarot-relationship-reading-v1",
      kind: "reading",
      generatedAt: now,
      questionSummary: request.question,
      relationshipStage: request.relationshipStage,
      tone: request.tone,
      formalVersion,
      cards: cards.map((card, index) => ({ ...card, contextualInterpretation: interpretation.cardInterpretations[index]! })),
      synthesis: interpretation.synthesis,
      relationshipDynamics: interpretation.relationshipDynamics,
      controllableFactors: interpretation.controllableFactors,
      uncontrollableFactors: interpretation.uncontrollableFactors,
      actionSuggestions: interpretation.actionSuggestions,
      reflectionQuestions: interpretation.reflectionQuestions,
      uncertainty: interpretation.uncertainty,
      safetyNotice: SAFETY_NOTICE,
      drawProof: proof,
    };
    return { ...withoutMarkdown, markdown: renderMarkdown(withoutMarkdown) } satisfies DeliverableArtifact;
  }
}

function overviewFor(request: ReadingRequest): unknown {
  return {
    schemaVersion: "overview-result-v1",
    understandingSummary: `你希望围绕“${request.question}”梳理当前关系互动，并获得尊重双方边界的行动视角。`,
    approach: [
      "使用固定三牌关系镜像牌阵，分别观察当前关系能量、核心互动模式和用户可采取的行动。",
      "将固定牌义与用户提供的背景结合，明确区分可观察事实、个人感受和象征性推断。",
      "同一正式任务的 V1–V3 保持原牌，只根据反馈深化解释，不通过反复抽牌追求特定答案。",
    ],
    deliverableStructure: [
      "三张牌及正逆位、基础牌义和关系语境解读",
      "关系互动线索、可控制与不可控制因素",
      "未来 72 小时及 7 天行动建议与反思问题",
      "抽牌证明、不确定性和安全说明",
    ],
    keyRisks: [
      "牌面不能验证对方未表达的真实想法，也不能保证复合、分手或其他未来结果。",
      "涉及暴力、自伤、跟踪、操控或未成年敏感关系时，将暂停或拒绝塔罗解读并优先提供安全指引。",
    ],
    estimatedDurationSeconds: 30,
    sample: "示例表达：牌面更适合作为一个需要核实的互动假设；真正的判断应回到双方可观察的行为、清晰沟通和现实边界。",
  };
}

function safetyArtifact(kind: "safety_redirect" | "declined", reasonCode: string, generatedAt: string): SafetyArtifact {
  const redirect = kind === "safety_redirect";
  const withoutMarkdown: Omit<SafetyArtifact, "markdown"> = {
    schemaVersion: "tarot-relationship-reading-v1",
    kind,
    generatedAt,
    reasonCode,
    message: redirect
      ? "你描述的情况可能涉及现实中的人身或心理安全风险。此时继续占卜可能会分散对紧急问题的注意，因此本次不会生成牌面判断。"
      : "这个请求涉及未成年人敏感关系、监控、操控或侵犯他人边界，不能通过塔罗提供帮助。",
    nextSteps: redirect
      ? ["如果存在立即危险，请离开危险环境并联系当地紧急服务。", "尽快联系一位可信任的人或专业支持人员，不要独自承担。", "保存威胁或伤害证据时优先保证自身安全。"]
      : ["把问题改写为你自己的感受、边界和可采取的健康行动。", "不要访问对方账号、位置或私人设备，也不要尝试操控其决定。"],
    safetyNotice: SAFETY_NOTICE,
  };
  return { ...withoutMarkdown, markdown: renderSafetyMarkdown(withoutMarkdown) };
}
