import type { TarotCard } from "./types.ts";

export const DECK_VERSION = "rws-zh-v1" as const;

const majorArcana: TarotCard[] = [
  major(0, "愚人", "The Fool", "开放、启程与允许未知发生", "冲动、逃避承诺或准备不足"),
  major(1, "魔术师", "The Magician", "主动表达、整合资源与把意图化为行动", "操控、言行不一或能力没有落实"),
  major(2, "女祭司", "The High Priestess", "内在感受、耐心观察与尚未说出的信息", "过度猜测、封闭或忽略直觉中的警讯"),
  major(3, "皇后", "The Empress", "滋养、接纳与关系中的丰盛感", "过度付出、依赖认可或照顾失衡"),
  major(4, "皇帝", "The Emperor", "边界、稳定与明确承担责任", "控制、僵化或权力不对等"),
  major(5, "教皇", "The Hierophant", "共同价值、承诺与可被讨论的规则", "因循惯例、价值冲突或被外界标准绑架"),
  major(6, "恋人", "The Lovers", "真诚选择、价值一致与关系连接", "选择回避、价值错位或关系中的分裂"),
  major(7, "战车", "The Chariot", "明确方向、协调差异并向前推进", "方向拉扯、急于控制结果或消耗过度"),
  major(8, "力量", "Strength", "温和坚定、耐心和情绪调节", "压抑、失去信心或用强硬掩盖脆弱"),
  major(9, "隐者", "The Hermit", "独处思考、诚实自省与放慢节奏", "孤立、回避沟通或反复内耗"),
  major(10, "命运之轮", "Wheel of Fortune", "关系周期变化与新的转折窗口", "重复旧模式、抗拒变化或把责任交给运气"),
  major(11, "正义", "Justice", "事实、互惠、责任与清晰决定", "双重标准、失衡或不愿面对行为后果"),
  major(12, "倒吊人", "The Hanged Man", "暂停、换位观察与放下无效控制", "无止境等待、自我牺牲或停滞"),
  major(13, "死神", "Death", "结束旧模式、转化与必要的告别", "抓住已结束的状态或害怕改变"),
  major(14, "节制", "Temperance", "协商、修复、节奏与差异整合", "沟通失调、情绪过量或难以找到中间地带"),
  major(15, "恶魔", "The Devil", "看见依附、欲望与不健康循环", "开始松绑，也可能是否认依赖问题"),
  major(16, "高塔", "The Tower", "真相打破旧结构并迫使关系重新评估", "压住危机、害怕揭露问题或延迟改变"),
  major(17, "星星", "The Star", "希望、坦诚疗愈与重新建立信任", "失望、理想化或暂时看不到方向"),
  major(18, "月亮", "The Moon", "复杂情绪、模糊信息与需要核实的感受", "迷雾逐渐散去，或仍在用猜测代替事实"),
  major(19, "太阳", "The Sun", "清晰、温暖、坦率与关系生命力", "快乐被遮挡、期待过高或表达不充分"),
  major(20, "审判", "Judgement", "复盘、觉察与基于经验作出新选择", "逃避复盘、自责过度或迟迟不作决定"),
  major(21, "世界", "The World", "阶段完成、整合经验与成熟连接", "未完成感、收尾困难或需要补足边界"),
];

const suits = [
  { id: "wands", zh: "权杖", en: "Wands", domain: "行动与热情", upright: "主动、创造力和推进意愿", reversed: "急躁、耗竭或行动方向分散" },
  { id: "cups", zh: "圣杯", en: "Cups", domain: "感受与连接", upright: "情绪流动、共情和亲密需要", reversed: "情绪堵塞、理想化或感受表达失衡" },
  { id: "swords", zh: "宝剑", en: "Swords", domain: "想法与沟通", upright: "辨别、表达和面对分歧", reversed: "误解、过度思考或言语伤害" },
  { id: "pentacles", zh: "星币", en: "Pentacles", domain: "现实与稳定", upright: "持续投入、可靠性和现实安排", reversed: "投入失衡、停滞或现实基础不足" },
] as const;

const ranks = [
  { id: "ace", zh: "王牌", en: "Ace", upright: "一个新的可能正在形成", reversed: "新的可能尚未准备好落地" },
  { id: "two", zh: "二", en: "Two", upright: "需要在双方或两个方向间建立协调", reversed: "选择困难或平衡正在被打破" },
  { id: "three", zh: "三", en: "Three", upright: "互动开始扩展并显现结果", reversed: "协作不足或期待没有对齐" },
  { id: "four", zh: "四", en: "Four", upright: "需要稳定、休整或守住已有基础", reversed: "停滞、封闭或安全感不足" },
  { id: "five", zh: "五", en: "Five", upright: "冲突或缺失促使双方看见真实需求", reversed: "冲突有缓和机会，但余波仍需处理" },
  { id: "six", zh: "六", en: "Six", upright: "关系出现调整、互助或向前移动", reversed: "过去模式拖慢进展或付出不对等" },
  { id: "seven", zh: "七", en: "Seven", upright: "需要辨别选择并保护重要边界", reversed: "逃避判断、选择过载或边界松动" },
  { id: "eight", zh: "八", en: "Eight", upright: "变化需要持续行动和专注练习", reversed: "行动受阻、重复无效努力或节奏混乱" },
  { id: "nine", zh: "九", en: "Nine", upright: "阶段接近成熟，同时需要照顾个人状态", reversed: "疲惫、防御或满足感被削弱" },
  { id: "ten", zh: "十", en: "Ten", upright: "一个周期达到结果并要求承担其影响", reversed: "负担过重、结局延迟或旧问题未清理" },
  { id: "page", zh: "侍从", en: "Page", upright: "以好奇和诚实开启一次学习或沟通", reversed: "表达不成熟、消息混乱或欠缺准备" },
  { id: "knight", zh: "骑士", en: "Knight", upright: "能量正在转化为明确行动", reversed: "行动过快、反复或只有姿态没有持续性" },
  { id: "queen", zh: "王后", en: "Queen", upright: "以成熟的内在方式承接这一主题", reversed: "内在需求被忽视，或以防御代替照顾" },
  { id: "king", zh: "国王", en: "King", upright: "以负责、稳定和清晰的方式处理这一主题", reversed: "权力使用失衡、固执或责任没有兑现" },
] as const;

function major(number: number, name: string, englishName: string, uprightMeaning: string, reversedMeaning: string): TarotCard {
  return { id: `major-${number.toString().padStart(2, "0")}`, name, englishName, arcana: "major", uprightMeaning, reversedMeaning };
}

const minorArcana: TarotCard[] = suits.flatMap((suit) => ranks.map((rank) => ({
  id: `${suit.id}-${rank.id}`,
  name: `${suit.zh}${rank.zh}`,
  englishName: `${rank.en} of ${suit.en}`,
  arcana: "minor" as const,
  uprightMeaning: `${rank.upright}；在${suit.domain}上体现为${suit.upright}`,
  reversedMeaning: `${rank.reversed}；在${suit.domain}上可能表现为${suit.reversed}`,
})));

export const TAROT_DECK: readonly TarotCard[] = Object.freeze([...majorArcana, ...minorArcana]);

if (TAROT_DECK.length !== 78 || new Set(TAROT_DECK.map((card) => card.id)).size !== 78) {
  throw new Error("tarot deck must contain 78 unique cards");
}
