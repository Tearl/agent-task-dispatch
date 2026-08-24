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

test("finance pages expose submitted, confirmation, refundable and terminal states accessibly", async () => {
  const [publisher,agent,reconciliation,adminLogin,adminLayout]=await Promise.all([
    readFile(new URL("../pages/publisher/Funds.tsx",import.meta.url),"utf8"),
    readFile(new URL("../pages/agent/Earnings.tsx",import.meta.url),"utf8"),
    readFile(new URL("../pages/admin/Reconciliation.tsx",import.meta.url),"utf8"),
    readFile(new URL("../pages/AdminLogin.tsx",import.meta.url),"utf8"),
    readFile(new URL("../layouts/AdminLayout.tsx",import.meta.url),"utf8"),
  ]);
  for(const page of [publisher,agent,reconciliation]) assert.match(page,/role="alert"/);
  assert.match(publisher,/submission === "submitted"/); assert.match(publisher,/confirmation/); assert.match(publisher,/refundStatus/); assert.match(publisher,/task\.terminal/);
  assert.match(agent,/formalClaimable/); assert.match(agent,/chainClaimable/); assert.match(agent,/controller/); assert.match(agent,/payout/);
  assert.match(reconciliation,/账本预期/); assert.match(reconciliation,/链上观测/); assert.doesNotMatch(reconciliation,/事件重放/);
  assert.match(adminLogin,/authenticateWallet/); assert.match(adminLogin,/revokeSession/); assert.doesNotMatch(adminLogin,/adminLogin|动态验证码/);
  assert.match(adminLayout,/authorizedRoles\.includes\("admin"\)/);
});

test("matching comparison uses authoritative snapshots and exposes degraded and pending states", async () => {
  const page = await readFile(new URL("../pages/publisher/AgentRecommendations.tsx", import.meta.url), "utf8");
  assert.match(page, /readMatchingView/);
  assert.match(page, /snapshot\?\.degradations/);
  assert.match(page, /aria-label="Agent 候选比较"/);
  assert.match(page, /aria-pressed=\{active\}/);
  assert.match(page, /status !== "valid"/);
  assert.match(page, /billingStatus !== "captured"/);
  assert.match(page, /检查链上确认/);
  assert.match(page, /operationID\.current/);
  for (const token of ["startMatching", "startOverview", "readTaskExecutions", "finalizeOverviewSlot", "权威执行状态"]) assert.match(page, new RegExp(token));
  assert.doesNotMatch(page, /from "\.\.\/\.\.\/lib\/mock"/);
});

test("primary workspaces load Engine read models instead of static fixtures",async()=>{
  const pages=await Promise.all(["publisher/Tasks.tsx","publisher/Dashboard.tsx","publisher/Marketplace.tsx","agent/Dashboard.tsx","agent/Integration.tsx","shared/Notifications.tsx"].map((name)=>readFile(new URL(`../pages/${name}`,import.meta.url),"utf8")));
  for(const page of pages)assert.doesNotMatch(page,/lib\/mock|TASKS|MY_AGENTS|NOTIFICATIONS|REVENUE_SERIES/);
  for(const token of ["readWorkspaceTasks","readMarketplaceAgents","readWorkspaceAgents","readWorkspaceNotifications"])assert.equal(pages.some((page)=>page.includes(token)),true,`${token} is not used`);
});

test("formal delivery UI exposes accessible append-only revision and three-state acceptance flows", async () => {
  const page=await readFile(new URL("../pages/publisher/Settlement.tsx",import.meta.url),"utf8");
  for(const token of ["readFormalDelivery","正式交付版本时间线","aria-pressed","结构化差异","feedback-description","change-description","intent_recorded","pending_confirmation","confirmed","检查链上确认","advanceWorkNonce","work nonce","responsibility_pending","awaiting_funding","role=\"alert\"","aria-live=\"polite\""]) assert.match(page,new RegExp(token));
  assert.match(page,/feedbackOperation\.current \?\?=/); assert.match(page,/changeOperation\.current \?\?=/);
  assert.match(page,/sessionStorage\.setItem/); assert.match(page,/sessionStorage\.getItem/); assert.match(page,/formal-delivery:\$\{kind\}:\$\{taskID\}/);
  assert.match(page,/disabled=\{!eligible\|\|confirmed\|\|busy\}/);
  assert.doesNotMatch(page,/from "\.\.\/\.\.\/lib\/mock"/);
});

test("dispute and administration workflows use authoritative cases, canonical freeze and audited repair paths",async()=>{
  const [workspace,review,admin,publisher,agent]=await Promise.all([readFile(new URL("../components/DisputeWorkspace.tsx",import.meta.url),"utf8"),readFile(new URL("../pages/arbitrator/CaseReview.tsx",import.meta.url),"utf8"),readFile(new URL("../pages/admin/Exceptions.tsx",import.meta.url),"utf8"),readFile(new URL("../pages/publisher/Disputes.tsx",import.meta.url),"utf8"),readFile(new URL("../pages/agent/Disputes.tsx",import.meta.url),"utf8")]);
  for(const token of ["readDisputes","争议案件时间线","soft_lock_pending","canonical DisputeFrozen","sessionStorage","appendDisputeEvidence","requestEvidenceAccess","settleDispute","双方签名和解","反请求","role=\"alert\""])assert.match(workspace,new RegExp(token));
  assert.match(review,/\[\s*0,\s*25,\s*50,\s*75,\s*100\s*\]/);assert.match(review,/reviewDispute/);assert.match(review,/evidenceRoot/);assert.match(review,/同一人员不能复核/);
  assert.match(admin,/runAdminOperation/);assert.match(admin,/dlq_replay/);assert.match(admin,/ledger_reversal/);assert.match(admin,/不能代签客户交易/);
  for(const page of [workspace,review,admin,publisher,agent])assert.doesNotMatch(page,/lib\/mock|CASES|toast\.success/);
});
