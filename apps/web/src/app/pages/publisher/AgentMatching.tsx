import {
  ArrowRight,
  Check,
  CheckCircle2,
  FileCheck2,
  Info,
  MessageSquareText,
  PencilLine,
  Save,
  Send,
  ShieldCheck,
  Sparkles,
  Target,
  X,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useLocation, useNavigate } from "react-router";
import { toast } from "sonner";

import { Page } from "../../components/AppShell";
import { GhostButton, InfoNote, PageHeader, Panel, Pill } from "../../components/kit/primitives";
import { analyzePublisherTask } from "../../lib/platform-api";
import {
  buildTaskAnalysis,
  type PublisherFlowState,
  type TaskAnalysis,
} from "../../lib/publisher-flow";

const FALLBACK_PROMPT = "抓取 3 个竞品官网的价格并整理成结构化表格";
const CATEGORIES = ["数据分析", "翻译", "图像生成", "代码开发", "市场研究", "智能审计"];
const QUICK_REFINEMENTS = ["预算调整为 1500 USDC", "交付周期改为 5 天", "增加 Excel 和 JSON 两种交付格式"];

type ConversationMessage = { id: number; role: "user" | "assistant"; content: string };
let messageSequence = 2;

export default function AgentMatching() {
  const navigate = useNavigate();
  const location = useLocation();
  const flowState = (location.state ?? {}) as PublisherFlowState;
  const prompt = flowState.prompt?.trim() || FALLBACK_PROMPT;
  const initialAnalysis = useMemo(
    () => flowState.analysis ?? buildTaskAnalysis(prompt, flowState.category, flowState.depth),
    [flowState.analysis, flowState.category, flowState.depth, prompt],
  );
  const [analysis, setAnalysis] = useState(initialAnalysis);
  const [analysisDraft, setAnalysisDraft] = useState(initialAnalysis);
  const [isEditingAnalysis, setIsEditingAnalysis] = useState(false);
  const [analysisRevision, setAnalysisRevision] = useState(flowState.analysisRevision ?? 1);
  const [message, setMessage] = useState("");
  const [isAnalyzing, setIsAnalyzing] = useState(!flowState.analysis);
  const [analysisModel, setAnalysisModel] = useState<string | null>(null);
  const initialRequestStarted = useRef(false);
  const [conversation, setConversation] = useState<ConversationMessage[]>([
    { id: 1, role: "user", content: prompt },
    { id: 2, role: "assistant", content: flowState.analysis ? "已恢复任务分析。你可以继续补充要求，确认后发布不可变任务规格。" : "正在请求 DeepSeek 生成第一版任务分析…" },
  ]);

  const loadInitialAnalysis = async () => {
    setIsAnalyzing(true);
    try {
      const result = await analyzePublisherTask({ prompt, category: flowState.category, depth: flowState.depth });
      setAnalysis(result.analysis);
      setAnalysisDraft(cloneAnalysis(result.analysis));
      setAnalysisModel(result.model);
      setConversation((items) => [...items, { id: nextMessageId(), role: "assistant", content: "DeepSeek 已生成第一版任务分析。你可以继续补充要求，确认后发布不可变任务规格。" }]);
    } catch (error) {
      const detail = error instanceof Error ? error.message : "模型服务暂时不可用";
      setConversation((items) => [...items, { id: nextMessageId(), role: "assistant", content: `AI 分析失败：${detail}。当前显示本地草稿，你可以重试。` }]);
      toast.error("DeepSeek 任务分析失败", { description: detail });
    } finally {
      setIsAnalyzing(false);
    }
  };

  useEffect(() => {
    if (flowState.analysis || initialRequestStarted.current) return;
    initialRequestStarted.current = true;
    void loadInitialAnalysis();
    // This request must run once per screen entry; including the local callback
    // would repeat a billable model call after every render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [flowState.analysis, flowState.category, flowState.depth, prompt]);

  const startEditingAnalysis = () => {
    setAnalysisDraft(cloneAnalysis(analysis));
    setIsEditingAnalysis(true);
  };

  const applyAnalysis = (next: TaskAnalysis, feedback: string) => {
    setAnalysis(next);
    setAnalysisDraft(cloneAnalysis(next));
    setAnalysisRevision((value) => value + 1);
    setConversation((items) => [...items, { id: nextMessageId(), role: "assistant", content: feedback }]);
  };

  const saveAnalysis = () => {
    if (!analysisDraft.title.trim() || !analysisDraft.summary.trim()) {
      toast.error("任务标题和任务摘要不能为空");
      return;
    }
    if (analysisDraft.budget <= 0 || analysisDraft.deliveryDays <= 0) {
      toast.error("预算和预计周期必须大于 0");
      return;
    }

    applyAnalysis(analysisDraft, "已应用你对分析结果的手动修改，请继续确认当前任务范围。");
    setIsEditingAnalysis(false);
    toast.success("分析结果已更新");
  };

  const sendMessage = async (preset?: string) => {
    const content = (preset ?? message).trim();
    if (!content) {
      toast.error("请输入需要补充或修改的要求");
      return;
    }

    setConversation((items) => [...items, { id: nextMessageId(), role: "user", content }]);
    setMessage("");
    setIsAnalyzing(true);
    try {
      const result = await analyzePublisherTask({ prompt, category: flowState.category, depth: flowState.depth, currentAnalysis: analysis, instruction: content });
      setAnalysisModel(result.model);
      applyAnalysis(result.analysis, describeChanges(analysis, result.analysis));
    } catch (error) {
      const detail = error instanceof Error ? error.message : "模型服务暂时不可用";
      setConversation((items) => [...items, { id: nextMessageId(), role: "assistant", content: `没有应用这次修改：${detail}` }]);
      toast.error("DeepSeek 更新分析失败", { description: detail });
    } finally {
      setIsAnalyzing(false);
    }
  };

  const continueToPublication = () => {
    navigate("/publisher/publish", {
      state: { ...flowState, prompt, analysis, analysisRevision, selectedAgentId: undefined } satisfies PublisherFlowState,
    });
  };

  return (
    <Page>
      <PageHeader
        title="AI 任务分析"
        subtitle="通过持续对话完善任务草稿，确认后先发布不可变规格，再读取权威匹配"
        actions={<GhostButton icon={PencilLine} onClick={() => navigate("/publisher")}>修改原始需求</GhostButton>}
      />

      <div className="flex items-center gap-3 rounded-2xl border border-[var(--ap-border)] bg-[rgba(10,18,38,0.45)] px-4 py-3">
        <FlowStep active icon={Sparkles} label="AI 任务分析" />
        <span className="h-px flex-1 bg-[var(--ap-border-strong)]" />
        <FlowStep icon={FileCheck2} label="发布规格" />
        <span className="h-px flex-1 bg-[var(--ap-border)]" />
        <FlowStep icon={ShieldCheck} label="匹配与选择" />
      </div>

      <Panel strong className="overflow-hidden">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--ap-border)] bg-[linear-gradient(120deg,rgba(34,211,238,.12),rgba(139,92,246,.08))] px-6 py-4">
          <div className="flex items-center gap-3">
            <Pill tone="cyan" dot>AI 分析结果</Pill>
            <span className="text-[11px] text-[var(--ap-muted)]">分析版本 R{analysisRevision} · {isAnalyzing ? "DeepSeek 分析中" : isEditingAnalysis ? "正在编辑" : "待你确认"}{analysisModel ? ` · ${analysisModel}` : ""}</span>
          </div>
          {!isEditingAnalysis ? (
            <div className="flex items-center gap-2">
              {!flowState.analysis && !analysisModel && !isAnalyzing ? <button type="button" onClick={() => void loadInitialAnalysis()} className="inline-flex items-center gap-1.5 rounded-lg border border-amber-300/25 px-3 py-2 text-[12px] text-amber-200 hover:border-amber-300/50"><Sparkles size={13} />重试 AI 分析</button> : null}
              <button type="button" disabled={isAnalyzing} onClick={startEditingAnalysis} className="inline-flex items-center gap-1.5 rounded-lg border border-cyan-300/25 bg-cyan-300/8 px-3 py-2 text-[12px] text-[var(--ap-cyan)] hover:border-cyan-300/50 disabled:cursor-not-allowed disabled:opacity-40">
                <PencilLine size={13} /> 编辑分析结果
              </button>
            </div>
          ) : null}
        </div>

        {isEditingAnalysis ? (
          <AnalysisEditor draft={analysisDraft} onChange={setAnalysisDraft} onCancel={() => setIsEditingAnalysis(false)} onSave={saveAnalysis} />
        ) : (
          <AnalysisResult analysis={analysis} prompt={prompt} />
        )}
      </Panel>

      <Panel strong className="overflow-hidden">
        <div className="flex items-center justify-between gap-3 border-b border-[var(--ap-border)] px-5 py-4">
          <div className="flex items-center gap-2 text-[15px] text-[var(--ap-text)]"><MessageSquareText size={17} className="text-[var(--ap-cyan)]" />继续和 AI 完善任务</div>
          <span className="text-[11px] text-[var(--ap-muted)]">每次补充都会生成新的任务分析版本</span>
        </div>

        <div className="max-h-64 space-y-3 overflow-y-auto px-5 py-4">
          {conversation.slice(-6).map((item) => (
            <div key={item.id} className={`flex ${item.role === "user" ? "justify-end" : "justify-start"}`}>
              <div className={`max-w-[78%] rounded-2xl px-4 py-2.5 text-[12.5px] leading-relaxed ${item.role === "user" ? "rounded-br-md bg-[var(--ap-cyan-soft)] text-[#baf5ff]" : "rounded-bl-md border border-[var(--ap-border)] bg-[rgba(5,9,20,.48)] text-[var(--ap-text-2)]"}`}>
                {item.content}
              </div>
            </div>
          ))}
        </div>

        <div className="border-t border-[var(--ap-border)] p-5">
          <div className="mb-3 flex flex-wrap gap-2">
            {QUICK_REFINEMENTS.map((item) => <button key={item} type="button" disabled={isAnalyzing} onClick={() => void sendMessage(item)} className="rounded-full border border-[var(--ap-border)] px-3 py-1.5 text-[11px] text-[var(--ap-muted)] hover:border-[var(--ap-border-strong)] hover:text-[var(--ap-text-2)] disabled:cursor-not-allowed disabled:opacity-40">{item}</button>)}
          </div>
          <div className="flex items-end gap-3 rounded-2xl border border-[var(--ap-border)] bg-[rgba(5,9,20,.55)] p-3 focus-within:border-[var(--ap-border-strong)]">
            <textarea
              value={message}
              onChange={(event) => setMessage(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) void sendMessage();
              }}
              rows={2}
              placeholder="继续补充：调整预算、修改周期、增加交付格式或补充验收要求…  ⌘/Ctrl + Enter 发送"
              className="min-h-14 flex-1 resize-none bg-transparent px-2 py-1 text-[13px] leading-relaxed text-white outline-none placeholder:text-[var(--ap-muted)]"
            />
            <button type="button" disabled={isAnalyzing} onClick={() => void sendMessage()} aria-label="发送补充要求" className="ap-cta grid h-10 w-10 shrink-0 place-items-center rounded-xl disabled:cursor-not-allowed disabled:opacity-40"><Send size={16} /></button>
          </div>
        </div>

        <div className="flex flex-wrap items-center justify-between gap-4 border-t border-[var(--ap-border)] bg-[rgba(10,18,38,.46)] px-5 py-4">
          <div className="min-w-0">
            <div className="text-[11px] text-[var(--ap-muted)]">即将锁定</div>
            <div className="mt-1 truncate text-[14px] text-[var(--ap-text)]">任务分析 R{analysisRevision}</div>
          </div>
          <button type="button" disabled={isEditingAnalysis || isAnalyzing} onClick={continueToPublication} className="ap-cta inline-flex items-center justify-center gap-2 rounded-xl px-5 py-2.5 text-[14px] disabled:cursor-not-allowed disabled:opacity-40">
            确认任务分析，发布任务规格 <ArrowRight size={16} />
          </button>
        </div>
      </Panel>
    </Page>
  );
}

function AnalysisResult({ analysis, prompt }: { analysis: TaskAnalysis; prompt: string }) {
  return (
    <div className="space-y-6 p-6">
      <div className="grid gap-6 xl:grid-cols-[minmax(0,1.4fr)_minmax(360px,.6fr)]">
        <div>
          <h2 className="text-[24px] leading-snug text-white">{analysis.title}</h2>
          <p className="mt-3 text-[13px] leading-relaxed text-[var(--ap-text-2)]">{analysis.summary}</p>
          <div className="mt-4 rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,.42)] px-4 py-3">
            <div className="text-[10px] uppercase tracking-[0.14em] text-[var(--ap-muted)]">原始需求</div>
            <p className="mt-1.5 text-[12px] leading-relaxed text-[var(--ap-text-2)]">“{prompt}”</p>
          </div>
        </div>
        <div className="grid grid-cols-3 gap-3 xl:grid-cols-1">
          <AnalysisMetric label="任务类型" value={analysis.category} />
          <AnalysisMetric label="建议预算" value={`${analysis.budget} USDC`} />
          <AnalysisMetric label="预计周期" value={`${analysis.deliveryDays} 天`} />
        </div>
      </div>

      <div className="grid gap-5 border-t border-[var(--ap-border)] pt-5 lg:grid-cols-3">
        <AnalysisList icon={FileCheck2} title="预期交付物" items={analysis.deliverables} />
        <AnalysisList icon={Target} title="验收标准" items={analysis.acceptanceCriteria} />
        <div className="space-y-4">
          <div><div className="mb-2 text-[12px] text-[var(--ap-muted)]">能力标签</div><div className="flex flex-wrap gap-2">{analysis.tags.map((tag) => <Pill key={tag} tone="violet">{tag}</Pill>)}<Pill tone="gray">{analysis.depth}分析</Pill></div></div>
          <InfoNote tone="amber"><span className="inline-flex items-start gap-2"><Info size={14} className="mt-0.5 shrink-0" />{analysis.risk}</span></InfoNote>
        </div>
      </div>
    </div>
  );
}

function AnalysisEditor({ draft, onChange, onCancel, onSave }: { draft: TaskAnalysis; onChange: (next: TaskAnalysis) => void; onCancel: () => void; onSave: () => void }) {
  const update = <K extends keyof TaskAnalysis>(key: K, value: TaskAnalysis[K]) => onChange({ ...draft, [key]: value });
  return (
    <div className="space-y-4 p-6">
      <div className="grid gap-4 lg:grid-cols-2"><EditField label="任务标题"><input value={draft.title} onChange={(event) => update("title", event.target.value)} className="analysis-edit-input" /></EditField><EditField label="任务类型"><select value={draft.category} onChange={(event) => update("category", event.target.value)} className="analysis-edit-input [color-scheme:dark]">{CATEGORIES.map((category) => <option key={category}>{category}</option>)}</select></EditField></div>
      <EditField label="任务摘要"><textarea value={draft.summary} onChange={(event) => update("summary", event.target.value)} rows={3} className="analysis-edit-input resize-none" /></EditField>
      <div className="grid gap-4 sm:grid-cols-2"><EditField label="建议预算"><div className="relative"><input type="number" min="1" value={draft.budget} onChange={(event) => update("budget", Number(event.target.value))} className="analysis-edit-input pr-14" /><span className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-[10px] text-[var(--ap-muted)]">USDC</span></div></EditField><EditField label="预计周期"><div className="relative"><input type="number" min="1" value={draft.deliveryDays} onChange={(event) => update("deliveryDays", Number(event.target.value))} className="analysis-edit-input pr-8" /><span className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-[10px] text-[var(--ap-muted)]">天</span></div></EditField></div>
      <div className="grid gap-4 lg:grid-cols-2"><EditField label="预期交付物（一行一项）"><textarea value={draft.deliverables.join("\n")} onChange={(event) => update("deliverables", lines(event.target.value))} rows={5} className="analysis-edit-input resize-none" /></EditField><EditField label="验收标准（一行一项）"><textarea value={draft.acceptanceCriteria.join("\n")} onChange={(event) => update("acceptanceCriteria", lines(event.target.value))} rows={5} className="analysis-edit-input resize-none" /></EditField></div>
      <div className="grid gap-4 lg:grid-cols-2"><EditField label="能力标签（使用逗号分隔）"><input value={draft.tags.join("，")} onChange={(event) => update("tags", event.target.value.split(/[,，]/).map((item) => item.trim()).filter(Boolean))} className="analysis-edit-input" /></EditField><EditField label="风险与待确认项"><textarea value={draft.risk} onChange={(event) => update("risk", event.target.value)} rows={2} className="analysis-edit-input resize-none" /></EditField></div>
      <div className="flex justify-end gap-2 border-t border-[var(--ap-border)] pt-4"><GhostButton icon={X} onClick={onCancel}>取消</GhostButton><button type="button" onClick={onSave} className="ap-cta inline-flex items-center gap-2 rounded-xl px-4 py-2.5 text-[13px]"><Save size={15} />保存分析</button></div>
    </div>
  );
}

function describeChanges(previous: TaskAnalysis, next: TaskAnalysis) {
  const changes: string[] = [];
  if (previous.budget !== next.budget) changes.push(`预算更新为 ${next.budget} USDC`);
  if (previous.deliveryDays !== next.deliveryDays) changes.push(`周期更新为 ${next.deliveryDays} 天`);
  if (previous.category !== next.category) changes.push(`任务类型更新为${next.category}`);
  if (previous.deliverables.length !== next.deliverables.length) changes.push("已补充交付物");
  if (previous.acceptanceCriteria.length !== next.acceptanceCriteria.length) changes.push("已补充验收标准");
  return `已理解并更新任务分析：${changes.length ? changes.join("、") : "已将补充要求写入任务草稿"}。请确认当前版本是否满足要求。`;
}

function cloneAnalysis(analysis: TaskAnalysis): TaskAnalysis { return { ...analysis, tags: [...analysis.tags], deliverables: [...analysis.deliverables], acceptanceCriteria: [...analysis.acceptanceCriteria] }; }
function nextMessageId() { messageSequence += 1; return messageSequence; }
function lines(value: string) { return value.split("\n").map((item) => item.trim()).filter(Boolean); }
function EditField({ label, children }: { label: string; children: ReactNode }) { return <label className="block"><span className="mb-1.5 block text-[11px] text-[var(--ap-muted)]">{label}</span>{children}</label>; }
function AnalysisMetric({ label, value }: { label: string; value: string }) { return <div className="rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,.38)] p-3"><div className="text-[10px] text-[var(--ap-muted)]">{label}</div><div className="mt-1 text-[13px] text-[var(--ap-text)]">{value}</div></div>; }
function AnalysisList({ icon: Icon, title, items }: { icon: typeof Target; title: string; items: string[] }) { return <div><div className="mb-3 flex items-center gap-2 text-[12px] text-[var(--ap-muted)]"><Icon size={14} className="text-[var(--ap-cyan)]" />{title}</div><ul className="space-y-2">{items.map((item) => <li key={item} className="flex items-start gap-2 text-[12px] leading-relaxed text-[var(--ap-text-2)]"><CheckCircle2 size={14} className="mt-0.5 shrink-0 text-[var(--ap-success)]" />{item}</li>)}</ul></div>; }
function FlowStep({ icon: Icon, label, active = false, done = false }: { icon: typeof Sparkles; label: string; active?: boolean; done?: boolean }) { return <div className={`flex shrink-0 items-center gap-2 text-[12px] ${active ? "text-[var(--ap-cyan)]" : "text-[var(--ap-muted)]"}`}><span className={`grid h-7 w-7 place-items-center rounded-full border ${active ? "border-cyan-300/40 bg-cyan-300/10" : "border-[var(--ap-border)]"}`}>{done ? <Check size={13} /> : <Icon size={13} />}</span><span className="hidden sm:inline">{label}</span></div>; }
