export type RelationshipStage = "single" | "crush" | "dating" | "committed" | "separated" | "post_breakup" | "self_reflection";
export type ReadingTone = "gentle" | "direct" | "neutral";
export type Orientation = "upright" | "reversed";
export type SafetyKind = "normal" | "safety_redirect" | "declined";

export interface ReadingRequest {
  relationshipStage: RelationshipStage;
  question: string;
  context?: string;
  feedback?: string;
  tone: ReadingTone;
  drawMode: "platform_random";
  ageConfirmed: true;
}

export interface TarotCard {
  id: string;
  name: string;
  englishName: string;
  arcana: "major" | "minor";
  uprightMeaning: string;
  reversedMeaning: string;
}

export interface DrawnCard extends TarotCard {
  position: string;
  orientation: Orientation;
  baseMeaning: string;
}

export interface DrawProof {
  algorithm: "sha256-counter-fisher-yates-v1";
  deckVersion: "rws-zh-v1";
  spreadVersion: "relationship-mirror-3-v1";
  scope: "overview" | "assignment";
  scopeId: string;
  seedDigest: string;
}

export interface InterpretationContent {
  cardInterpretations: string[];
  synthesis: string;
  relationshipDynamics: string[];
  controllableFactors: string[];
  uncontrollableFactors: string[];
  actionSuggestions: string[];
  reflectionQuestions: string[];
  uncertainty: string;
}

export interface ReadingArtifact {
  schemaVersion: "tarot-relationship-reading-v1";
  kind: "reading";
  generatedAt: string;
  questionSummary: string;
  relationshipStage: RelationshipStage;
  tone: ReadingTone;
  formalVersion: number;
  cards: Array<DrawnCard & { contextualInterpretation: string }>;
  synthesis: string;
  relationshipDynamics: string[];
  controllableFactors: string[];
  uncontrollableFactors: string[];
  actionSuggestions: string[];
  reflectionQuestions: string[];
  uncertainty: string;
  safetyNotice: string;
  drawProof: DrawProof;
  markdown: string;
}

export interface SafetyArtifact {
  schemaVersion: "tarot-relationship-reading-v1";
  kind: "safety_redirect" | "declined";
  generatedAt: string;
  reasonCode: string;
  message: string;
  nextSteps: string[];
  safetyNotice: string;
  markdown: string;
}

export type DeliverableArtifact = ReadingArtifact | SafetyArtifact;
