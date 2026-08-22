export interface TaskAnalysis {
  title: string;
  summary: string;
  category: string;
  depth: string;
  budget: number;
  deliveryDays: number;
  tags: string[];
  deliverables: string[];
  acceptanceCriteria: string[];
  risk: string;
}

export interface PublisherFlowState {
  taskId?: string;
  prompt?: string;
  category?: string | null;
  depth?: string;
  analysis?: TaskAnalysis;
  analysisRevision?: number;
  selectedAgentId?: string;
}

const CATEGORY_RULES = [
  { category: "智能审计", keywords: ["审计", "合约", "安全", "漏洞"] },
  { category: "翻译", keywords: ["翻译", "本地化", "语言", "语种"] },
  { category: "图像生成", keywords: ["图片", "图像", "主图", "视觉", "海报"] },
  { category: "代码开发", keywords: ["代码", "开发", "接口", "重构", "单测"] },
  { category: "市场研究", keywords: ["市场", "调研", "消费者", "竞品分析"] },
] as const;

const CATEGORY_DETAILS: Record<
  string,
  { budget: number; tags: string[]; deliverables: string[]; criteria: string[]; risk: string }
> = {
  数据分析: {
    budget: 1200,
    tags: ["数据采集", "结构化", "质量校验"],
    deliverables: ["结构化数据文件", "字段说明与数据字典", "采集与清洗说明"],
    criteria: ["目标数据完整率不低于 95%", "字段格式与约定 Schema 一致", "抽样数据可追溯并可复验"],
    risk: "目标站点可能存在访问限制，需在执行前确认数据源可用性。",
  },
  翻译: {
    budget: 800,
    tags: ["多语言", "术语一致性", "本地化"],
    deliverables: ["完整译文", "双语术语表", "翻译质量检查报告"],
    criteria: ["内容无缺译漏译", "核心术语全篇一致", "交付格式保持原文结构"],
    risk: "行业术语需要用户提供现有词库或品牌规范。",
  },
  图像生成: {
    budget: 1000,
    tags: ["视觉生成", "风格一致", "批量交付"],
    deliverables: ["完整尺寸成图", "缩略预览与编号清单", "生成参数与风格说明"],
    criteria: ["数量与尺寸符合要求", "系列视觉风格保持一致", "交付文件无明显生成瑕疵"],
    risk: "品牌素材和商用授权范围需要在生成前确认。",
  },
  代码开发: {
    budget: 1800,
    tags: ["工程实现", "自动化测试", "交付文档"],
    deliverables: ["可运行源代码", "自动化测试", "部署与使用说明"],
    criteria: ["核心功能按需求运行", "类型检查与测试通过", "不存在高危安全问题"],
    risk: "需要明确现有代码仓库、运行环境和第三方依赖版本。",
  },
  市场研究: {
    budget: 1500,
    tags: ["市场洞察", "多源验证", "研究报告"],
    deliverables: ["研究报告", "数据来源清单", "关键结论与行动建议"],
    criteria: ["关键结论有来源支撑", "覆盖约定市场与竞品", "数据口径和时间范围明确"],
    risk: "部分市场数据可能依赖付费数据库或存在时效限制。",
  },
  智能审计: {
    budget: 3200,
    tags: ["安全审计", "风险分级", "修复建议"],
    deliverables: ["安全审计报告", "问题复现说明", "分级修复建议"],
    criteria: ["覆盖约定代码范围", "风险可复现且分级明确", "高危问题提供可执行修复方案"],
    risk: "审计结论依赖冻结的代码版本，后续代码变更需重新评估。",
  },
};

export function buildTaskAnalysis(prompt: string, selectedCategory?: string | null, depth = "深度"): TaskAnalysis {
  const normalizedPrompt = prompt.trim() || "抓取 3 个竞品官网的价格并整理成结构化表格";
  const inferredCategory =
    selectedCategory ||
    CATEGORY_RULES.find((rule) => rule.keywords.some((keyword) => normalizedPrompt.includes(keyword)))?.category ||
    "数据分析";
  const detail = CATEGORY_DETAILS[inferredCategory] ?? CATEGORY_DETAILS["数据分析"];
  const deliveryDays = depth === "专家" ? 5 : depth === "标准" ? 2 : 3;
  const title = normalizedPrompt.length > 26 ? `${normalizedPrompt.slice(0, 26)}…` : normalizedPrompt;

  return {
    title,
    summary: `基于用户描述，完成“${normalizedPrompt}”，并以可验证、可复用的形式交付结果。`,
    category: inferredCategory,
    depth,
    budget: detail.budget,
    deliveryDays,
    tags: detail.tags,
    deliverables: detail.deliverables,
    acceptanceCriteria: detail.criteria,
    risk: detail.risk,
  };
}

export function refineTaskAnalysis(current: TaskAnalysis, instruction: string): TaskAnalysis {
  const next = {
    ...current,
    tags: [...current.tags],
    deliverables: [...current.deliverables],
    acceptanceCriteria: [...current.acceptanceCriteria],
  };
  const budget = instruction.match(/(?:预算|控制在|不超过)\D{0,8}(\d{2,8})/);
  const deliveryDays = instruction.match(/(?:周期|交付|完成|改为|调整为)\D{0,8}(\d{1,3})\s*天/);
  const inferredCategory = CATEGORY_RULES.find((rule) => rule.keywords.some((keyword) => instruction.includes(keyword)))?.category;

  if (budget) next.budget = Number(budget[1]);
  if (deliveryDays) next.deliveryDays = Number(deliveryDays[1]);
  if (inferredCategory && instruction.includes("类型")) next.category = inferredCategory;

  if (/(?:增加|新增|还要|补充).*(?:交付|文件|格式|报告|表格|数据)/.test(instruction)) {
    next.deliverables = appendUnique(next.deliverables, instruction);
  } else if (/(?:验收|标准|必须|确保|要求)/.test(instruction)) {
    next.acceptanceCriteria = appendUnique(next.acceptanceCriteria, instruction);
  } else if (!budget && !deliveryDays) {
    next.acceptanceCriteria = appendUnique(next.acceptanceCriteria, instruction);
  }

  next.summary = `${current.summary.replace(/\n补充要求：.*$/s, "")}\n补充要求：${instruction}`;
  return next;
}

function appendUnique(items: string[], value: string) {
  const normalized = value.trim();
  return items.includes(normalized) ? items : [...items, normalized];
}
