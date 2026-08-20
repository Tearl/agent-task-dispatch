import {
  AlertTriangle,
  BarChart3,
  Bell,
  Boxes,
  ClipboardList,
  Code2,
  Coins,
  Cpu,
  FilePlus2,
  Gavel,
  LayoutDashboard,
  Link2,
  ListChecks,
  MessageSquareWarning,
  PackageCheck,
  RefreshCcw,
  Scale,
  ScrollText,
  ServerCog,
  ShieldCheck,
  SlidersHorizontal,
  Sparkles,
  Store,
  UserCog,
  Users,
  Vote,
  Wallet,
  type LucideIcon,
} from "lucide-react";

export type RoleId = "publisher" | "agent" | "arbitrator" | "admin";

export interface NavItem {
  to: string;
  label: string;
  icon: LucideIcon;
}

export interface RoleConfig {
  id: RoleId;
  name: string;
  short: string;
  accent: string;
  home: string;
  nav: NavItem[];
}

export const ROLES: Record<RoleId, RoleConfig> = {
  publisher: {
    id: "publisher",
    name: "任务发布方",
    short: "发布方",
    accent: "#22d3ee",
    home: "/publisher",
    nav: [
      { to: "/publisher", label: "工作台", icon: LayoutDashboard },
      { to: "/publisher/publish", label: "发布任务与托管", icon: FilePlus2 },
      { to: "/publisher/matching", label: "AI 任务分析", icon: Sparkles },
      { to: "/publisher/recommendations", label: "Agent 推荐", icon: Cpu },
      { to: "/publisher/settlement", label: "交付与结算", icon: PackageCheck },
      { to: "/publisher/marketplace", label: "Agent 大厅", icon: Store },
      { to: "/publisher/tasks", label: "我的任务", icon: ListChecks },
      { to: "/publisher/funds", label: "资金与收益", icon: Wallet },
      { to: "/publisher/disputes", label: "争议处理", icon: MessageSquareWarning },
    ],
  },
  agent: {
    id: "agent",
    name: "Agent 开发者",
    short: "开发者",
    accent: "#8b5cf6",
    home: "/agent",
    nav: [
      { to: "/agent", label: "Agent 工作台", icon: LayoutDashboard },
      { to: "/agent/orders", label: "任务订单", icon: ClipboardList },
      { to: "/agent/integration", label: "Agent 管理与接入", icon: Boxes },
      { to: "/agent/developer", label: "开发者中心", icon: Code2 },
      { to: "/agent/reputation", label: "数据与信誉", icon: BarChart3 },
      { to: "/agent/earnings", label: "收益中心", icon: Coins },
      { to: "/agent/disputes", label: "争议处理", icon: MessageSquareWarning },
    ],
  },
  arbitrator: {
    id: "arbitrator",
    name: "仲裁者",
    short: "仲裁者",
    accent: "#34d399",
    home: "/arbitrator",
    nav: [
      { to: "/arbitrator", label: "仲裁工作台", icon: LayoutDashboard },
      { to: "/arbitrator/cases", label: "待处理案件", icon: Gavel },
      { to: "/arbitrator/review", label: "案件审理", icon: Scale },
      { to: "/arbitrator/appeal", label: "申诉与复核", icon: RefreshCcw },
      { to: "/arbitrator/staking", label: "质押与奖励", icon: ShieldCheck },
      { to: "/arbitrator/governance", label: "社区治理", icon: Vote },
    ],
  },
  admin: {
    id: "admin",
    name: "平台管理员",
    short: "管理员",
    accent: "#38bdf8",
    home: "/admin",
    nav: [
      { to: "/admin", label: "治理概览", icon: LayoutDashboard },
      { to: "/admin/users", label: "用户与角色", icon: Users },
      { to: "/admin/agents", label: "Agent 治理", icon: Cpu },
      { to: "/admin/exceptions", label: "异常任务", icon: AlertTriangle },
      { to: "/admin/reconciliation", label: "链上对账", icon: Link2 },
      { to: "/admin/audit", label: "审计日志", icon: ScrollText },
      { to: "/admin/config", label: "系统配置", icon: SlidersHorizontal },
    ],
  },
};

export const CLIENT_ROLES: RoleId[] = ["publisher", "agent", "arbitrator"];

export const SHARED_NAV: NavItem[] = [
  { to: "/account", label: "账户与安全", icon: UserCog },
  { to: "/notifications", label: "消息中心", icon: Bell },
];

export const ADMIN_ICON: LucideIcon = ServerCog;
