import { createRuntime } from "./bootstrap.ts";
import { assertProductionConfig, loadConfig } from "./config.ts";
import { createAgentServer } from "./http/server.ts";

const config = loadConfig();
assertProductionConfig(config);
const runtime = createRuntime(config);
const server = createAgentServer(runtime.executions, runtime.artifacts, config.apiToken);

server.listen(config.port, "0.0.0.0", () => {
  process.stdout.write(`tarot-relationship agent listening on :${config.port}\n`);
});

for (const signal of ["SIGINT", "SIGTERM"] as const) {
  process.on(signal, () => server.close(() => process.exit(0)));
}
