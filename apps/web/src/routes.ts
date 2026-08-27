import { index, layout, route, type RouteConfig } from "@react-router/dev/routes";

export default [
  index("routes/_index.tsx"),
  route("admin/login", "app/pages/AdminLogin.tsx"),

  layout("app/layouts/PublisherLayout.tsx", [
    route("publisher", "app/pages/publisher/Dashboard.tsx"),
    route("publisher/publish", "app/pages/publisher/PublishTask.tsx"),
    route("publisher/matching", "app/pages/publisher/AgentMatching.tsx"),
    route("publisher/recommendations", "app/pages/publisher/AgentRecommendations.tsx"),
		route("publisher/settlement", "app/pages/publisher/LegacySettlement.tsx"),
		route("publisher/tasks/:taskId/delivery", "app/pages/publisher/Settlement.tsx"),
    route("publisher/marketplace", "app/pages/publisher/Marketplace.tsx"),
    route("publisher/tasks", "app/pages/publisher/Tasks.tsx"),
		route("publisher/tasks/:taskId/funding", "app/pages/publisher/TaskFunding.tsx"),
    route("publisher/funds", "app/pages/publisher/Funds.tsx"),
    route("publisher/disputes", "app/pages/publisher/Disputes.tsx"),
  ]),

  layout("app/layouts/AgentLayout.tsx", [
    route("agent", "app/pages/agent/Dashboard.tsx"),
    route("agent/orders", "app/pages/agent/Orders.tsx"),
    route("agent/integration", "app/pages/agent/Integration.tsx"),
    route("agent/integration/new", "app/pages/agent/OnboardAgent.tsx"),
    route("agent/developer", "app/pages/agent/DeveloperCenter.tsx"),
    route("agent/reputation", "app/pages/agent/Reputation.tsx"),
    route("agent/earnings", "app/pages/agent/Earnings.tsx"),
    route("agent/disputes", "app/pages/agent/Disputes.tsx"),
  ]),

  layout("app/layouts/ArbitratorLayout.tsx", [
    route("arbitrator", "app/pages/arbitrator/Dashboard.tsx"),
    route("arbitrator/cases", "app/pages/arbitrator/PendingCases.tsx"),
    route("arbitrator/review", "app/pages/arbitrator/CaseReview.tsx"),
    route("arbitrator/appeal", "app/pages/arbitrator/Appeal.tsx"),
    route("arbitrator/staking", "app/pages/arbitrator/Staking.tsx"),
    route("arbitrator/governance", "app/pages/arbitrator/Governance.tsx"),
  ]),

  layout("app/layouts/SharedLayout.tsx", [
    route("account", "app/pages/shared/Account.tsx"),
    route("notifications", "app/pages/shared/Notifications.tsx"),
  ]),

  layout("app/layouts/AdminLayout.tsx", [
    route("admin", "app/pages/admin/Dashboard.tsx"),
    route("admin/users", "app/pages/admin/Users.tsx"),
    route("admin/agents", "app/pages/admin/Agents.tsx"),
    route("admin/exceptions", "app/pages/admin/Exceptions.tsx"),
    route("admin/reconciliation", "app/pages/admin/Reconciliation.tsx"),
    route("admin/audit", "app/pages/admin/Audit.tsx"),
    route("admin/config", "app/pages/admin/Config.tsx"),
  ]),
] satisfies RouteConfig;
