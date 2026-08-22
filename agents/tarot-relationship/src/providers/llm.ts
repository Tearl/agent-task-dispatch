import { CompatibleChatProvider, type JsonGenerator } from "@agent-platform/agent-runtime";
import { interpretationSchema } from "../domain/schema.ts";
import type { InterpretationContent } from "../domain/types.ts";
import type { InterpretationInput, RelationshipInterpreter } from "../interpretation/interpreter.ts";

export class CompatibleRelationshipInterpreter implements RelationshipInterpreter {
  private readonly generator: JsonGenerator;

  constructor(
    baseUrl: string,
    model: string,
    apiKey?: string,
    timeoutMs = 60_000,
  ) {
    this.generator = new CompatibleChatProvider({
      baseUrl,
      model,
      apiKey,
      timeoutMs,
      temperature: 0.2,
    });
  }

  async interpret(input: InterpretationInput): Promise<InterpretationContent> {
    return interpretationSchema.parse(await this.generator.generate(systemPrompt, JSON.stringify(input)));
  }
}

const systemPrompt = `你是一个关系反思型塔罗解读器。输入包含用户问题、三张已经确定的牌和正式版本号。
只返回 JSON，字段必须是 cardInterpretations(恰好3项)、synthesis、relationshipDynamics、controllableFactors、uncontrollableFactors、actionSuggestions、reflectionQuestions、uncertainty。
必须遵守：
1. 塔罗仅是象征性反思框架，不是事实来源。
2. 不声称知道第三方真实想法，不预测确定结果，不承诺复合、出轨或命定关系。
3. 明确区分可观察事实、用户感受和象征性假设。
4. 建议必须尊重同意、隐私与边界，禁止操控、跟踪、试探或报复。
5. V2/V3 只深化原牌，不建议重新抽牌。
6. 使用简体中文，避免恐吓和宿命化表达。`;
