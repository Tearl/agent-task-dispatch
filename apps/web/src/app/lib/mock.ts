import type { ReputationDims } from "../components/kit/ReputationRadar";

export type TaskStatus =
  | "draft"
  | "escrowed"
  | "matching"
  | "in_progress"
  | "delivered"
  | "settled"
  | "disputed"
  | "refunded";

export const STATUS_LABEL: Record<TaskStatus, string> = {
  draft: "草稿",
  escrowed: "已托管待匹配",
  matching: "匹配中",
  in_progress: "执行中",
  delivered: "待验收",
  settled: "已结算",
  disputed: "争议中",
  refunded: "已退款",
};

export const STATUS_TONE: Record<
  TaskStatus,
  "gray" | "cyan" | "violet" | "blue" | "amber" | "green" | "red"
> = {
  draft: "gray",
  escrowed: "cyan",
  matching: "violet",
  in_progress: "blue",
  delivered: "amber",
  settled: "green",
  disputed: "red",
  refunded: "gray",
};

export interface Task {
  id: string;
  title: string;
  category: string;
  status: TaskStatus;
  amount: number;
  yield: number;
  agent?: string;
  progress: number;
  deadline: string;
  next?: string;
}

export const TASKS: Task[] = [
  {
    id: "TSK-2048",
    title: "跨境电商竞品数据抓取与结构化",
    category: "数据分析",
    status: "in_progress",
    amount: 1200,
    yield: 8.4,
    agent: "DataForge",
    progress: 62,
    deadline: "2026-08-24",
    next: "等待 Agent 交付",
  },
  {
    id: "TSK-2041",
    title: "产品说明书 8 语种本地化翻译",
    category: "翻译",
    status: "delivered",
    amount: 640,
    yield: 3.1,
    agent: "LinguaX",
    progress: 100,
    deadline: "2026-08-21",
    next: "请验收结果",
  },
  {
    id: "TSK-2033",
    title: "品牌视觉主图批量生成（50 张）",
    category: "图像生成",
    status: "matching",
    amount: 980,
    yield: 0,
    progress: 0,
    deadline: "2026-08-27",
    next: "选择候选 Agent",
  },
  {
    id: "TSK-2020",
    title: "智能合约安全审计（DeFi 质押池）",
    category: "智能审计",
    status: "settled",
    amount: 3200,
    yield: 22.6,
    agent: "AuditNode",
    progress: 100,
    deadline: "2026-08-12",
    next: "已完成",
  },
  {
    id: "TSK-2012",
    title: "目标市场消费者调研报告",
    category: "市场研究",
    status: "disputed",
    amount: 1500,
    yield: 11.2,
    agent: "InsightBot",
    progress: 90,
    deadline: "2026-08-15",
    next: "争议审理中",
  },
  {
    id: "TSK-2001",
    title: "后端订单服务重构与单测补齐",
    category: "代码开发",
    status: "escrowed",
    amount: 2600,
    yield: 1.2,
    progress: 0,
    deadline: "2026-08-30",
    next: "匹配中",
  },
];

export interface AgentCandidate {
  id: string;
  name: string;
  tagline: string;
  match: number;
  price: number;
  eta: string;
  success: number;
  reputation: ReputationDims;
  reasons: string[];
  category: string;
  calls: number;
}

export const AGENTS: AgentCandidate[] = [
  {
    id: "AG-01",
    name: "DataForge",
    tagline: "大规模网页抓取与数据清洗专家",
    match: 96,
    price: 1120,
    eta: "约 2 天",
    success: 98.4,
    category: "数据分析",
    calls: 12840,
    reputation: { quality: 95, speed: 88, reliability: 96, communication: 90, compliance: 99 },
    reasons: ["历史同类任务成功率 98%", "支持结构化 Schema 校验", "响应中位数 4 分钟"],
  },
  {
    id: "AG-02",
    name: "InsightBot",
    tagline: "消费者洞察与市场调研自动化",
    match: 91,
    price: 1260,
    eta: "约 3 天",
    success: 95.1,
    category: "市场研究",
    calls: 8620,
    reputation: { quality: 92, speed: 84, reliability: 90, communication: 94, compliance: 96 },
    reasons: ["多源数据交叉验证", "内置可解释报告模板", "近 30 天零超时"],
  },
  {
    id: "AG-03",
    name: "QuantScope",
    tagline: "数据建模与预测分析",
    match: 87,
    price: 990,
    eta: "约 2.5 天",
    success: 93.7,
    category: "数据分析",
    calls: 6410,
    reputation: { quality: 90, speed: 92, reliability: 86, communication: 82, compliance: 95 },
    reasons: ["价格更优", "擅长时序预测", "提供中间过程审计日志"],
  },
];

export interface DisputeCase {
  id: string;
  task: string;
  status: "evidence" | "voting" | "sealed" | "ruled" | "appeal";
  frozen: number;
  deadline: string;
  role: string;
  summary: string;
}

export const CASES: DisputeCase[] = [
  {
    id: "ARB-771",
    task: "TSK-2012 · 市场调研报告",
    status: "voting",
    frozen: 1500,
    deadline: "12:24:07",
    role: "需求方 vs Agent",
    summary: "交付内容与验收标准第 3 条覆盖度存在分歧",
  },
  {
    id: "ARB-765",
    task: "TSK-1998 · 图像批量生成",
    status: "evidence",
    frozen: 720,
    deadline: "31:02:55",
    role: "需求方 vs Agent",
    summary: "交付图片分辨率不达标，Agent 主张需求变更",
  },
  {
    id: "ARB-758",
    task: "TSK-1974 · 合约审计",
    status: "sealed",
    frozen: 3200,
    deadline: "已封存",
    role: "需求方 vs Agent",
    summary: "密封投票已完成，等待到期揭晓",
  },
  {
    id: "ARB-742",
    task: "TSK-1930 · 代码重构",
    status: "ruled",
    frozen: 0,
    deadline: "已裁决",
    role: "需求方 vs Agent",
    summary: "裁决：按 70% 完成度结算，30% 退款",
  },
];

export interface Notification {
  id: string;
  type: "task" | "fund" | "dispute" | "security";
  title: string;
  time: string;
  read: boolean;
}

export const NOTIFICATIONS: Notification[] = [
  { id: "N1", type: "task", title: "TSK-2041 已交付，请在 48 小时内验收", time: "10 分钟前", read: false },
  { id: "N2", type: "fund", title: "TSK-2020 已结算，本金与收益已入账", time: "1 小时前", read: false },
  { id: "N3", type: "dispute", title: "ARB-771 进入密封投票阶段", time: "3 小时前", read: true },
  { id: "N4", type: "security", title: "检测到新设备登录（上海）已通过签名验证", time: "昨天", read: true },
  { id: "N5", type: "task", title: "TSK-2033 收到 3 个候选 Agent 推荐", time: "昨天", read: true },
];

export const YIELD_SERIES = [
  { d: "周一", v: 12 },
  { d: "周二", v: 18 },
  { d: "周三", v: 15 },
  { d: "周四", v: 26 },
  { d: "周五", v: 22 },
  { d: "周六", v: 31 },
  { d: "周日", v: 28 },
];

export const REVENUE_SERIES = [
  { d: "3月", v: 4200 },
  { d: "4月", v: 5100 },
  { d: "5月", v: 4800 },
  { d: "6月", v: 6400 },
  { d: "7月", v: 7200 },
  { d: "8月", v: 8100 },
];

export interface AgentInstance {
  id: string;
  name: string;
  category: string;
  status: "online" | "degraded" | "offline";
  health: number;
  endpoint: string;
  protocol: string;
  orders30d: number;
}

export const MY_AGENTS: AgentInstance[] = [
  {
    id: "AG-01",
    name: "DataForge",
    category: "数据分析",
    status: "online",
    health: 99,
    endpoint: "https://api.dataforge.ai/v1/invoke",
    protocol: "OpenAPI 3.1 ✓",
    orders30d: 214,
  },
  {
    id: "AG-07",
    name: "LinguaX",
    category: "翻译",
    status: "degraded",
    health: 82,
    endpoint: "https://api.linguax.io/run",
    protocol: "OpenAPI 3.1 ✓",
    orders30d: 96,
  },
  {
    id: "AG-11",
    name: "PixForge",
    category: "图像生成",
    status: "offline",
    health: 0,
    endpoint: "https://pixforge.app/api/gen",
    protocol: "校验失败",
    orders30d: 0,
  },
];
