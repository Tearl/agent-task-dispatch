import { TarotExecutionService } from "./application/execution-service.ts";
import { TarotReadingService } from "./application/reading-service.ts";
import type { AgentConfig } from "./config.ts";
import { GuardedInterpreter, TemplateRelationshipInterpreter, type RelationshipInterpreter } from "./interpretation/interpreter.ts";
import { HttpCallbackSender, NoopCallbackSender } from "./protocol/callback.ts";
import { SafeInputResolver } from "./protocol/input-resolver.ts";
import { CompatibleRelationshipInterpreter } from "./providers/llm.ts";
import { FileArtifactStore } from "./storage/artifact-store.ts";

export function createRuntime(config: AgentConfig): {
  executions: TarotExecutionService;
  artifacts: FileArtifactStore;
} {
  const artifacts = new FileArtifactStore(config.dataDir, config.publicBaseUrl);
  const interpreter = createInterpreter(config);
  return {
    artifacts,
    executions: new TarotExecutionService(
      new TarotReadingService(interpreter),
      new SafeInputResolver(),
      artifacts,
      config.callbackKey ? new HttpCallbackSender(config.callbackKey) : new NoopCallbackSender(),
      config.callbackKeyVersion,
    ),
  };
}

function createInterpreter(config: AgentConfig): RelationshipInterpreter {
  const fallback = new TemplateRelationshipInterpreter();
  if (!config.llmModel) return fallback;
  return new GuardedInterpreter(
    new CompatibleRelationshipInterpreter(config.llmBaseUrl, config.llmModel, config.llmApiKey, config.llmTimeoutMs),
    fallback,
  );
}
