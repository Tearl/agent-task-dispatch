import assert from "node:assert/strict";
import test from "node:test";
import { TAROT_DECK } from "../src/domain/cards.ts";
import { TarotReadingService } from "../src/application/reading-service.ts";
import { GuardedInterpreter, TemplateRelationshipInterpreter, type RelationshipInterpreter } from "../src/interpretation/interpreter.ts";
import { drawRelationshipSpread } from "../src/randomness/deterministic-draw.ts";
import { classifySafety } from "../src/security/safety.ts";

const taskSpecHash = `sha256:${"a".repeat(64)}`;
const request = {
  relationshipStage: "dating" as const,
  question: "最近沟通减少，我应该主动联系吗？",
  context: "交往半年，最近两周联系频率下降。",
  tone: "gentle" as const,
  drawMode: "platform_random" as const,
  ageConfirmed: true as const,
};

test("deck contains 78 unique cards and deterministic draws never duplicate", () => {
  assert.equal(TAROT_DECK.length, 78);
  assert.equal(new Set(TAROT_DECK.map((card) => card.id)).size, 78);
  const first = drawRelationshipSpread({ taskSpecHash, scope: "assignment", scopeId: "assignment-1" });
  const replay = drawRelationshipSpread({ taskSpecHash, scope: "assignment", scopeId: "assignment-1" });
  const other = drawRelationshipSpread({ taskSpecHash, scope: "assignment", scopeId: "assignment-2" });
  assert.deepEqual(first, replay);
  assert.equal(new Set(first.cards.map((card) => card.id)).size, 3);
  assert.notEqual(first.proof.seedDigest, other.proof.seedDigest);
});

test("formal V1 and V3 retain the same cards for one assignment", async () => {
  const service = new TarotReadingService(new TemplateRelationshipInterpreter());
  const first = await service.execute({ taskSpecHash, stage: "formal", scopeId: "assignment-1", formalVersion: 1, body: request, now: new Date("2026-08-22T00:00:00Z") });
  const third = await service.execute({ taskSpecHash, stage: "formal", scopeId: "assignment-1", formalVersion: 3, body: { ...request, feedback: "请把行动建议说得更直接。" }, now: new Date("2026-08-22T00:01:00Z") });
  assert.equal((first as { kind: string }).kind, "reading");
  assert.deepEqual(
    (first as { cards: Array<{ id: string; orientation: string }> }).cards.map(({ id, orientation }) => ({ id, orientation })),
    (third as { cards: Array<{ id: string; orientation: string }> }).cards.map(({ id, orientation }) => ({ id, orientation })),
  );
});

test("safety classifier redirects immediate risk and declines surveillance", () => {
  assert.deepEqual(classifySafety({ ...request, question: "分手后我不想活了" }), { kind: "safety_redirect", reasonCode: "immediate_safety_risk" });
  assert.deepEqual(classifySafety({ ...request, question: "怎么偷偷定位对方的位置" }), { kind: "declined", reasonCode: "coercion_or_surveillance" });
  assert.deepEqual(classifySafety(request), { kind: "normal" });
});

test("unsafe model certainty falls back to bounded local interpretation", async () => {
  const unsafe: RelationshipInterpreter = {
    async interpret() {
      return {
        cardInterpretations: ["他一定爱你", "他一定会复合", "你们命中注定"],
        synthesis: "他一定会复合",
        relationshipDynamics: ["命中注定"],
        controllableFactors: ["等待"],
        uncontrollableFactors: ["结果"],
        actionSuggestions: ["等待"],
        reflectionQuestions: ["何时复合？"],
        uncertainty: "没有不确定性",
      };
    },
  };
  const service = new TarotReadingService(new GuardedInterpreter(unsafe));
  const artifact = await service.execute({ taskSpecHash, stage: "formal", scopeId: "assignment-1", body: request }) as { synthesis: string; uncertainty: string };
  assert.doesNotMatch(artifact.synthesis, /一定会复合|命中注定/u);
  assert.match(artifact.uncertainty, /不能保证/u);
});

test("overview reveals method but not a drawn card", async () => {
  const service = new TarotReadingService(new TemplateRelationshipInterpreter());
  const overview = await service.execute({ taskSpecHash, stage: "overview", scopeId: "task-1", body: request }) as Record<string, unknown>;
  assert.equal(overview.schemaVersion, "overview-result-v1");
  assert.equal("cards" in overview, false);
  assert.match(String(overview.sample), /象征性|互动假设/u);
});

test("user text cannot inject raw HTML into rendered Markdown", async () => {
  const service = new TarotReadingService(new TemplateRelationshipInterpreter());
  const artifact = await service.execute({
    taskSpecHash,
    stage: "formal",
    scopeId: "assignment-html",
    body: { ...request, question: "<script>alert(1)</script> 我该怎么办？" },
  }) as { markdown: string };
  assert.doesNotMatch(artifact.markdown, /<script>/u);
  assert.match(artifact.markdown, /&lt;script&gt;/u);
});
