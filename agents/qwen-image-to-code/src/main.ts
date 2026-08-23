import {
  assertExecutionAdapterProductionConfig,
  createAgentExecutionServer,
  loadExecutionAdapterConfig,
} from "@agent-platform/agent-runtime";
import { createPlatformRuntime } from "./platform.ts";

const prefix = "QWEN_IMAGE_TO_CODE_AGENT";
const config = loadExecutionAdapterConfig({
  environmentPrefix: prefix,
  defaultPort: 8094,
  defaultDataDir: ".data/qwen-image-to-code",
});
assertExecutionAdapterProductionConfig(config, prefix);
const runtime = createPlatformRuntime(config);
const server = createAgentExecutionServer({
  manifest: { id: "qwen-image-to-code", version: "0.1.0" },
  ...runtime,
  apiToken: config.apiToken,
});

server.listen(config.port, "0.0.0.0", () => process.stdout.write(`qwen-image-to-code agent listening on :${config.port}\n`));
for (const signal of ["SIGINT", "SIGTERM"] as const) {
  process.on(signal, () => server.close(() => process.exit(0)));
}
