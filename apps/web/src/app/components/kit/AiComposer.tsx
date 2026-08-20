import {
  ArrowUp,
  BarChart3,
  ChevronDown,
  Code2,
  Gauge,
  Image as ImageIcon,
  Languages,
  Mic,
  Paperclip,
  ScanLine,
  Search,
  Sparkles,
  Wallet,
  Wand2,
  type LucideIcon,
} from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router";
import { toast } from "sonner";

const CATEGORIES: { label: string; icon: LucideIcon; color: string }[] = [
  { label: "数据分析", icon: BarChart3, color: "#22d3ee" },
  { label: "翻译", icon: Languages, color: "#38bdf8" },
  { label: "图像生成", icon: ImageIcon, color: "#8b5cf6" },
  { label: "代码开发", icon: Code2, color: "#22d3ee" },
  { label: "市场研究", icon: Search, color: "#8b5cf6" },
  { label: "智能审计", icon: ScanLine, color: "#34d399" },
];

const EXAMPLES = [
  "抓取 3 个竞品官网的价格并整理成结构化表格",
  "把这份产品说明书翻译成 8 种语言并保持术语一致",
  "为新品牌生成 50 张统一风格的电商主图",
  "审计我们的质押合约，输出安全风险报告",
];

const DEPTH = ["标准", "深度", "专家"];

export function AiComposer() {
  const navigate = useNavigate();
  const [text, setText] = useState("");
  const [category, setCategory] = useState<string | null>(null);
  const [depth, setDepth] = useState("深度");
  const [depthOpen, setDepthOpen] = useState(false);

  const submit = () => {
    if (!text.trim()) {
      toast.error("先用一句话描述你的任务");
      return;
    }

    toast.success("AI 正在解析需求并匹配专业 Agent…");
    navigate("/publisher/matching", { state: { prompt: text, category, depth } });
  };

  return (
    <div className="relative overflow-x-clip">
      <div
        className="pointer-events-none absolute -inset-x-10 -top-16 h-56 opacity-70 blur-3xl"
        style={{
          background:
            "radial-gradient(60% 100% at 50% 0%, rgba(34,211,238,0.25), transparent 70%), radial-gradient(50% 100% at 80% 20%, rgba(139,92,246,0.22), transparent 70%)",
        }}
      />

      <div className="relative text-center">
        <div className="ap-glass inline-flex items-center gap-2 rounded-full px-3.5 py-1.5 text-[12px] text-[var(--ap-cyan)]">
          <Sparkles size={13} /> AI 托管 · 自动匹配专业 Agent
        </div>
        <h1 className="mt-4 text-[clamp(28px,3.4vw,44px)] leading-tight tracking-tight">
          <span className="ap-text-gradient">一句话</span>
          <span className="text-white">，发布你的任务</span>
        </h1>
        <p className="mt-2 text-[15px] text-[var(--ap-text-2)]">
          描述需求，AI 帮你拆解、托管资金并匹配最合适的 Agent 完成
        </p>
      </div>

      <div className="ap-glass-strong ap-ring-glow ap-rise-in relative mt-7 rounded-3xl p-4">
        <div
          className="pointer-events-none absolute inset-0 rounded-3xl"
          style={{
            padding: 1,
            background:
              "linear-gradient(120deg, rgba(34,211,238,0.5), rgba(139,92,246,0.4), transparent)",
            WebkitMask: "linear-gradient(#000 0 0) content-box, linear-gradient(#000 0 0)",
            WebkitMaskComposite: "xor",
            maskComposite: "exclude",
          }}
        />
        <textarea
          value={text}
          onChange={(event) => setText(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) submit();
          }}
          rows={3}
          placeholder="例如：抓取竞品价格数据并整理成表格，预算 1200 USDC，3 天内交付…    ⌘/Ctrl + Enter 发布"
          className="relative w-full resize-none bg-transparent px-3 pt-2 text-[15px] leading-relaxed text-white outline-none placeholder:text-[var(--ap-muted)]"
        />

        <div className="relative mt-2 flex flex-wrap items-center justify-between gap-2">
          <div className="flex flex-wrap items-center gap-2">
            <div className="relative">
              <button
                type="button"
                onClick={() => setDepthOpen((value) => !value)}
                className="inline-flex items-center gap-1.5 rounded-xl border border-[var(--ap-border)] px-3 py-2 text-[13px] text-[var(--ap-text-2)] hover:border-[var(--ap-border-strong)]"
              >
                <Gauge size={15} className="text-[var(--ap-cyan)]" /> {depth} <ChevronDown size={13} />
              </button>
              {depthOpen ? (
                <div className="ap-glass-strong absolute left-0 top-11 z-20 w-32 rounded-xl p-1.5">
                  {DEPTH.map((item) => (
                    <button
                      key={item}
                      type="button"
                      onClick={() => {
                        setDepth(item);
                        setDepthOpen(false);
                      }}
                      className="block w-full rounded-lg px-3 py-2 text-left text-[13px] text-[var(--ap-text-2)] hover:bg-[rgba(34,211,238,0.06)]"
                    >
                      {item}分析
                    </button>
                  ))}
                </div>
              ) : null}
            </div>
            <Chip icon={Paperclip} label="上传" onClick={() => toast.success("可上传参考文件辅助 AI 理解需求")} />
            <Chip icon={Wand2} label="技能" onClick={() => toast.success("可 @ 指定技能，如 @数据清洗")} />
            <Chip icon={Wallet} label="托管预算" onClick={() => navigate("/publisher/publish")} />
          </div>

          <div className="flex items-center gap-2">
            <button
              type="button"
              aria-label="语音输入"
              className="grid h-10 w-10 place-items-center rounded-xl border border-[var(--ap-border)] text-[var(--ap-muted)] hover:border-[var(--ap-border-strong)]"
            >
              <Mic size={17} />
            </button>
            <button
              type="button"
              aria-label="发布任务"
              onClick={submit}
              className="ap-cta grid h-10 w-10 place-items-center rounded-xl"
            >
              <ArrowUp size={18} />
            </button>
          </div>
        </div>
      </div>

      <div className="mt-5 flex flex-wrap items-center justify-center gap-2.5">
        {CATEGORIES.map((item) => {
          const active = category === item.label;
          return (
            <button
              key={item.label}
              type="button"
              onClick={() => setCategory(active ? null : item.label)}
              className="ap-hoverable inline-flex items-center gap-2 rounded-full border px-4 py-2 text-[13px] transition-colors"
              style={{
                borderColor: active ? item.color : "var(--ap-border)",
                background: active ? `${item.color}1f` : "rgba(10,18,38,0.5)",
                color: active ? "#e8f0ff" : "var(--ap-text-2)",
              }}
            >
              <item.icon size={15} style={{ color: item.color }} /> {item.label}
            </button>
          );
        })}
      </div>

      <div className="mt-5">
        <div className="mb-2.5 text-center text-[12px] text-[var(--ap-muted)]">试试这些示例</div>
        <div className="flex flex-wrap justify-center gap-2">
          {EXAMPLES.map((example) => (
            <button
              key={example}
              type="button"
              onClick={() => setText(example)}
              className="ap-glass ap-hoverable rounded-xl px-3.5 py-2 text-left text-[12.5px] text-[var(--ap-text-2)]"
            >
              <Sparkles size={12} className="mr-1.5 inline text-[var(--ap-violet)]" />
              {example}
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}

function Chip({ icon: Icon, label, onClick }: { icon: LucideIcon; label: string; onClick?: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="inline-flex items-center gap-1.5 rounded-xl border border-[var(--ap-border)] px-3 py-2 text-[13px] text-[var(--ap-text-2)] hover:border-[var(--ap-border-strong)]"
    >
      <Icon size={15} className="text-[var(--ap-cyan)]" /> {label}
    </button>
  );
}
