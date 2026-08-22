import { interpretationSchema } from "../domain/schema.ts";
import type { DrawnCard, InterpretationContent, ReadingRequest } from "../domain/types.ts";
import { containsProhibitedClaim } from "../security/safety.ts";

export interface InterpretationInput {
  request: ReadingRequest;
  cards: DrawnCard[];
  formalVersion: number;
}

export interface RelationshipInterpreter {
  interpret(input: InterpretationInput): Promise<InterpretationContent>;
}

export class TemplateRelationshipInterpreter implements RelationshipInterpreter {
  async interpret(input: InterpretationInput): Promise<InterpretationContent> {
    const [current, pattern, action] = input.cards;
    if (!current || !pattern || !action) throw new Error("relationship spread requires three cards");
    const toneLead = input.request.tone === "direct"
      ? "直接来看"
      : input.request.tone === "neutral"
        ? "从中性视角来看"
        : "温和地看";
    const versionLead = input.formalVersion > 1 && input.request.feedback
      ? `结合你的补充反馈“${shorten(input.request.feedback, 80)}”，这次仍围绕原来的三张牌继续澄清。`
      : "这组三张牌用于整理关系线索，而不是读取另一方未经表达的真实想法。";

    return {
      cardInterpretations: [
        `${current.name}${orientationLabel(current)}落在“${current.position}”，提示${current.baseMeaning}。先观察近期可验证的沟通和行为，而不是只根据情绪猜测。`,
        `${pattern.name}${orientationLabel(pattern)}显示“${pattern.position}”可能与${pattern.baseMeaning}有关。它更像一个值得验证的互动假设，并非对任何人的定论。`,
        `${action.name}${orientationLabel(action)}把注意力拉回你可控制的部分：${action.baseMeaning}。行动宜小、清楚且尊重双方边界。`,
      ],
      synthesis: `${toneLead}，${versionLead} 当前的关键不是预测关系会走向哪里，而是分清事实、感受和期待。${current.name}描述当下体验，${pattern.name}提醒需要核实的互动模式，${action.name}则建议把选择建立在清晰表达和对方实际回应上。`,
      relationshipDynamics: [
        `关系中可能同时存在“${current.baseMeaning}”与“${pattern.baseMeaning}”两股力量。`,
        "沟通频率本身不是全部信息，更重要的是双方是否愿意回应需求、说明边界并保持行动一致。",
      ],
      controllableFactors: [
        "你可以选择表达方式、联系频率以及愿意接受的关系边界。",
        "你可以把观察到的事实和自己的感受分开，再提出一个具体、容易回应的问题。",
      ],
      uncontrollableFactors: [
        "你无法通过牌面确认对方没有说出口的想法，也不能控制对方是否回应。",
        "关系结果需要双方真实行动共同形成，任何一次解读都不能保证结果。",
      ],
      actionSuggestions: [
        `未来 72 小时内，尝试一次低压力、无指责的沟通，例如先描述事实，再表达感受和一个具体请求。`,
        `未来 7 天观察对方是否有持续、可验证的回应；若长期只有你单方面维持，把边界和自己的需要放回决策中心。`,
      ],
      reflectionQuestions: [
        "我现在最想获得的是事实、安慰，还是一个确定答案？",
        "如果只看对方最近的实际行动，而不看我的猜测，我会怎样描述这段关系？",
        "什么样的回应能够让我感到被尊重，什么情况意味着我需要后退一步？",
      ],
      uncertainty: "塔罗提供的是象征性的反思框架。上述内容不能验证他人的真实想法，也不能保证复合、分手或其他未来事件。",
    };
  }
}

export class GuardedInterpreter implements RelationshipInterpreter {
  private readonly primary: RelationshipInterpreter;
  private readonly fallback: RelationshipInterpreter;

  constructor(
    primary: RelationshipInterpreter,
    fallback: RelationshipInterpreter = new TemplateRelationshipInterpreter(),
  ) {
    this.primary = primary;
    this.fallback = fallback;
  }

  async interpret(input: InterpretationInput): Promise<InterpretationContent> {
    try {
      const value = interpretationSchema.parse(await this.primary.interpret(input));
      if (containsProhibitedClaim(value)) throw new Error("interpretation contains prohibited certainty claim");
      return value;
    } catch {
      return this.fallback.interpret(input);
    }
  }
}

function orientationLabel(card: DrawnCard): string {
  return card.orientation === "upright" ? "（正位）" : "（逆位）";
}

function shorten(value: string, maximum: number): string {
  return value.length <= maximum ? value : `${value.slice(0, maximum - 1)}…`;
}
