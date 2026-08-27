import { createRuntime } from "./bootstrap.ts";
import { assertConfig, loadConfig } from "./config.ts";
import { createQwenUiPrototypeServer } from "./server.ts";

const config = loadConfig();
assertConfig(config);
const runtime = createRuntime(config);
const server = createQwenUiPrototypeServer(runtime.jobs, runtime.images, config.apiToken, runtime.executions, runtime.artifacts);
server.listen(config.port, "0.0.0.0", () => process.stdout.write(`qwen-ui-prototype agent listening on :${config.port}\n`));
for (const signal of ["SIGINT", "SIGTERM"] as const) process.on(signal, () => server.close(() => process.exit(0)));
