import type { ReadingRequest, SafetyKind } from "../domain/types.ts";

export interface SafetyDecision {
  kind: SafetyKind;
  reasonCode?: string;
}

const crisisPatterns = [
  /自杀|轻生|不想活|结束生命|伤害自己|割腕|跳楼|同归于尽/u,
  /杀了?他|杀了?她|弄死|伤害对方|报复.*伤害/u,
];

const violencePatterns = [
  /家暴|殴打|掐脖|勒住|威胁.*生命|限制人身自由|强迫发生关系/u,
];

const manipulationPatterns = [
  /跟踪|监控|偷看.*手机|定位对方|装定位|窃听/u,
  /操控|控制对方|让.*离不开我|让.*听话|报复|下药/u,
];

const minorPatterns = [/未成年|初中生|小学生|不到十八|17岁|16岁|15岁|14岁/u];

export function classifySafety(request: ReadingRequest): SafetyDecision {
  const text = `${request.question}\n${request.context ?? ""}\n${request.feedback ?? ""}`.toLowerCase();
  if (matches(text, crisisPatterns)) return { kind: "safety_redirect", reasonCode: "immediate_safety_risk" };
  if (matches(text, violencePatterns)) return { kind: "safety_redirect", reasonCode: "relationship_violence_risk" };
  if (matches(text, minorPatterns)) return { kind: "declined", reasonCode: "minor_sensitive_relationship" };
  if (matches(text, manipulationPatterns)) return { kind: "declined", reasonCode: "coercion_or_surveillance" };
  return { kind: "normal" };
}

export function containsProhibitedClaim(value: unknown): boolean {
  const text = JSON.stringify(value);
  return [
    /(?:一定|必然|百分之百|绝对)(?:会|不会|爱|不爱|复合|分手|出轨)/u,
    /他(?:现在)?(?:肯定|一定|绝对)(?:在想|爱|不爱|会)/u,
    /她(?:现在)?(?:肯定|一定|绝对)(?:在想|爱|不爱|会)/u,
    /命中注定|唯一的灵魂伴侣|保证复合/u,
  ].some((pattern) => pattern.test(text));
}

function matches(value: string, patterns: RegExp[]): boolean {
  return patterns.some((pattern) => pattern.test(value));
}
