import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

test("wallet and mutation controls expose accessible pending and error states", async () => {
  const [home, agent, task] = await Promise.all([
    readFile(new URL("../../routes/_index.tsx", import.meta.url), "utf8"),
    readFile(new URL("../pages/agent/OnboardAgent.tsx", import.meta.url), "utf8"),
    readFile(new URL("../pages/publisher/PublishTask.tsx", import.meta.url), "utf8"),
  ]);
  assert.match(home, /disabled=\{connecting\}/);
  assert.match(home, /aria-busy=\{connecting\}/);
  assert.match(home, /connectInFlight\.current/);
  assert.match(home, /aria-label="登录角色"/);
  assert.match(home, /aria-pressed=\{active\}/);
  assert.match(agent, /disabled=\{submitting \|\| allPassed\}/);
  assert.match(task, /disabled=\{submitting \|\| Boolean\(publication\)\}/);
  assert.match(agent, /submitInFlight\.current/);
  assert.match(task, /submitInFlight\.current/);
  assert.match(agent, /role="alert"/);
  assert.match(task, /role="alert"/);
  assert.match(agent, /放弃本次操作并重新开始/);
  assert.match(task, /放弃本次操作并重新编辑/);
});

test("core forms retain labels, keyboard buttons, and responsive single-column fallbacks", async () => {
  const [agent, task] = await Promise.all([
    readFile(new URL("../pages/agent/OnboardAgent.tsx", import.meta.url), "utf8"),
    readFile(new URL("../pages/publisher/PublishTask.tsx", import.meta.url), "utf8"),
  ]);
  for (const id of ["agent-name", "agent-tagline", "agent-endpoint", "agent-concurrency", "agent-secret", "agent-overview-price", "agent-formal-price"]) {
    assert.match(agent, new RegExp(`htmlFor="${id}"`));
    assert.match(agent, new RegExp(`id="${id}"`));
  }
  for (const id of ["task-title", "task-description", "task-criteria", "task-budget", "task-deadline"]) {
    assert.match(task, new RegExp(`htmlFor="${id}"`));
    assert.match(task, new RegExp(`id="${id}"`));
  }
  assert.match(agent, /grid-cols-1[^\n]*sm:grid-cols-2/);
  assert.match(task, /grid-cols-1[^\n]*sm:grid-cols-2/);
  assert.doesNotMatch(agent, /onClick=\{runChecks\}[^>]*pointer-events-none/);
  assert.match(agent, /aria-current=\{active \? "step" : undefined\}/);
  assert.match(agent, /aria-label="能力分类"/);
  assert.match(agent, /aria-label="鉴权方式"/);
  assert.match(task, /aria-label="任务分类"/);
});

test("an Agent attempt locks mutable input until the user explicitly abandons the idempotent operation", async () => {
  const agent = await readFile(new URL("../pages/agent/OnboardAgent.tsx", import.meta.url), "utf8");
  assert.match(agent, /const attemptLocked = Boolean\(operationId\.current\)/);
  assert.match(agent, /disabled=\{submitting \|\| attemptLocked\}/);
  assert.match(agent, /为保证幂等重试，当前输入已锁定/);
  assert.match(agent, /operationId\.current = undefined/);
});

test("development and production API calls stay same-origin and proxy to the configured BFF", async () => {
  const [viteConfig, productionServer, packageJson, api] = await Promise.all([
    readFile(new URL("../../../vite.config.ts", import.meta.url), "utf8"),
    readFile(new URL("../../../server.mjs", import.meta.url), "utf8"),
    readFile(new URL("../../../package.json", import.meta.url), "utf8"),
    readFile(new URL("./platform-api.ts", import.meta.url), "utf8"),
  ]);
  assert.match(viteConfig, /VITE_BFF_URL/);
  assert.match(viteConfig, /["']\/api["']/);
  assert.match(viteConfig, /proxy/);
  assert.match(productionServer, /BFF_URL/);
  assert.match(productionServer, /pathname\.startsWith\("\/api\/"\)/);
  assert.match(packageJson, /"start": "node \.\/server\.mjs"/);
  assert.doesNotMatch(api, /VITE_BFF_URL|localhost:3000/);
});

test("session restoration exposes a retry state instead of redirecting transient failures", async () => {
  const [session, publisher, agent, shared, recovery] = await Promise.all([
    readFile(new URL("./session.tsx", import.meta.url), "utf8"),
    readFile(new URL("../layouts/PublisherLayout.tsx", import.meta.url), "utf8"),
    readFile(new URL("../layouts/AgentLayout.tsx", import.meta.url), "utf8"),
    readFile(new URL("../layouts/SharedLayout.tsx", import.meta.url), "utf8"),
    readFile(new URL("../components/SessionRecovery.tsx", import.meta.url), "utf8"),
  ]);
  assert.match(session, /setRestoreError\(error instanceof Error/);
  assert.match(session, /retrySession/);
  assert.match(session, /generation !== restoreGeneration\.current/);
  assert.match(session, /restoreGeneration\.current \+= 1/);
  for (const layout of [publisher, agent, shared]) assert.match(layout, /if \(restoreError\) return <SessionRecovery/);
  assert.match(recovery, /role="alert"/);
  assert.match(recovery, /重试会话恢复/);
});

test("browser requests an Engine health probe without asserting the result", async () => {
  const [api, onboarding] = await Promise.all([
    readFile(new URL("./platform-api.ts", import.meta.url), "utf8"),
    readFile(new URL("../pages/agent/OnboardAgent.tsx", import.meta.url), "utf8"),
  ]);
  assert.match(api, /endpointUrl: input\.endpointUrl/);
  assert.match(api, /health:\$\{refreshVersion\}/);
  assert.doesNotMatch(api, /health:\s*["']healthy["']/);
  assert.match(onboarding, /htmlFor="agent-endpoint"/);
});

test("task publication submits the independently confirmed acceptance criteria", async () => {
  const task = await readFile(new URL("../pages/publisher/PublishTask.tsx", import.meta.url), "utf8");
  assert.match(task, /criteria: criteriaText\.split/);
  assert.match(task, /htmlFor="task-criteria"/);
  assert.doesNotMatch(task, /criteria: analysis\?\.acceptanceCriteria/);
});
