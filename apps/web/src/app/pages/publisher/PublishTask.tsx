import { useRef, useState } from 'react';
import { useLocation, useNavigate } from 'react-router';
import { toast } from 'sonner';
import { ShieldCheck, Lock, Info, FileCheck2 } from 'lucide-react';
import { Page } from '../../components/AppShell';
import { PageHeader, Panel, SectionTitle, CtaButton, GhostButton, InfoNote, Pill } from '../../components/kit/primitives';
import type { PublisherFlowState } from '../../lib/publisher-flow';
import { createAndPublishTask, prepareTaskFunding, readTaskFunding, recordTaskFundingSubmission, submitTaskFundingTransaction, validateTaskPublishInput, type TaskFundingIntent, type TaskPublishInput, type WalletProvider } from '../../lib/platform-api';

const CATEGORIES = ['数据分析', '翻译', '图像生成', '代码开发', '市场研究', '智能审计'];

export default function PublishTask() {
  const location = useLocation();
  const navigate = useNavigate();
  const flowState = (location.state ?? {}) as PublisherFlowState;
  const analysis = flowState.analysis;
  const [title, setTitle] = useState(analysis?.title ?? '');
  const [cat, setCat] = useState(analysis?.category ?? '数据分析');
  const [amount, setAmount] = useState(String(analysis?.budget ?? 1200));
  const [desc, setDesc] = useState(
    analysis
      ? `${analysis.summary}\n\n交付物：\n${analysis.deliverables.map((item) => `- ${item}`).join('\n')}`
      : '',
  );
  const [criteriaText, setCriteriaText] = useState(
    (analysis?.acceptanceCriteria?.length ? analysis.acceptanceCriteria : ['交付内容符合任务描述', '结果格式完整且可验证']).join('\n'),
  );
  const [deadline, setDeadline] = useState(new Date(Date.now() + 7 * 86_400_000).toISOString().slice(0, 10));
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [publication, setPublication] = useState<{ taskId: string; specHash: string; acceptanceHash: string } | null>(null);
  const [funding, setFunding] = useState<TaskFundingIntent | null>(null);
  const [fundingBusy, setFundingBusy] = useState(false);
  const operationId = useRef<string | undefined>(undefined);
  const submitInFlight = useRef(false);
  const fee = Math.round(Number(amount || 0) * 0.02);

  const submit = async () => {
    if (submitInFlight.current || publication) return;
    submitInFlight.current = true;
    setError(null);
    if (!title.trim() || !desc.trim() || !criteriaText.trim() || !amount || !deadline) {
      submitInFlight.current = false;
      setError('请完整填写标题、描述、验收标准、预算和截止日期。');
      return;
    }
    setSubmitting(true);
    try {
      const input: TaskPublishInput = {
        operationId: operationId.current ?? crypto.randomUUID(),
        title: title.trim(), description: desc.trim(), category: cat, tags: analysis?.tags ?? [], amount, deadline,
        criteria: criteriaText.split('\n').map((item) => item.trim()).filter(Boolean),
      };
      validateTaskPublishInput(input);
      operationId.current = input.operationId;
      const result = await createAndPublishTask(input);
      setPublication({ taskId: result.task.id, specHash: result.spec.contentHash, acceptanceHash: result.acceptance.contentHash });
      toast.success('任务规格与验收版本已不可变发布');
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '任务发布失败，请重试。');
    } finally {
      submitInFlight.current = false;
      setSubmitting(false);
    }
  };

  const resetAttempt = () => {
    submitInFlight.current = false;
    operationId.current = undefined;
    setError(null);
  };

  const fundTask = async () => {
    if (!publication || fundingBusy) return;
    const ethereum = (window as typeof window & { ethereum?: WalletProvider }).ethereum;
    if (!ethereum) { setError('未检测到以太坊兼容钱包。'); return; }
    setFundingBusy(true); setError(null);
    try {
      let current = funding;
      if (!current) current = await prepareTaskFunding(publication.taskId, `${operationId.current}:funding`);
      setFunding(current);
      if (current.status === 'prepared' || current.status === 'orphaned') {
        const transactionHash = await submitTaskFundingTransaction(ethereum, current);
        current = await recordTaskFundingSubmission(publication.taskId, current, transactionHash);
        setFunding(current);
      } else if (current.status === 'submitted') {
        current = await readTaskFunding(publication.taskId);
        setFunding(current);
      }
      if (current.status === 'confirmed') {
        toast.success('托管交易已达到确认深度，任务可以开始匹配');
      } else {
        toast.info('托管交易已提交，请稍后同步链上确认');
      }
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '任务托管失败，请重试。');
    } finally { setFundingBusy(false); }
  };

  return (
    <Page>
      <PageHeader title="确认并发布任务规格" subtitle="发布资格由 Engine 判定；发布后规格与加权验收版本不可覆盖" />

      <div className="grid gap-6 lg:grid-cols-[1.5fr_1fr]">
        <Panel className="p-6 space-y-5">
          <SectionTitle>任务信息</SectionTitle>
          <div>
            <label htmlFor="task-title" className="text-[13px] text-[var(--ap-muted)]">任务标题</label>
            <input
              id="task-title"
              disabled={Boolean(operationId.current)}
              value={title} onChange={(e) => setTitle(e.target.value)}
              placeholder="例如：跨境电商竞品数据抓取与结构化"
              className="mt-2 w-full rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.5)] px-4 py-3 text-[14px] text-white outline-none focus:border-[var(--ap-border-strong)]"
            />
          </div>
          <div>
            <div className="text-[13px] font-medium text-[var(--ap-muted)]">任务分类</div>
            <div role="group" aria-label="任务分类" className="mt-2 flex flex-wrap gap-2">
              {CATEGORIES.map((c) => (
                <button
                  key={c} onClick={() => setCat(c)}
                  type="button"
                  aria-pressed={cat === c}
                  disabled={Boolean(operationId.current)}
                  className="rounded-lg border px-3 py-2 text-[13px] transition-colors"
                  style={{
                    borderColor: cat === c ? 'var(--ap-border-strong)' : 'var(--ap-border)',
                    background: cat === c ? 'var(--ap-cyan-soft)' : 'transparent',
                    color: cat === c ? '#a5f3fc' : 'var(--ap-text-2)',
                  }}
                >{c}</button>
              ))}
            </div>
          </div>
          <div>
            <label htmlFor="task-description" className="text-[13px] text-[var(--ap-muted)]">需求描述与交付物</label>
            <textarea
              id="task-description"
              disabled={Boolean(operationId.current)}
              value={desc} onChange={(e) => setDesc(e.target.value)} rows={5}
              placeholder="描述任务范围、交付物与格式…"
              className="mt-2 w-full resize-none rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.5)] px-4 py-3 text-[14px] text-white outline-none focus:border-[var(--ap-border-strong)]"
            />
          </div>
          <div>
            <label htmlFor="task-criteria" className="text-[13px] text-[var(--ap-muted)]">确认验收标准（每行一条）</label>
            <textarea
              id="task-criteria"
              disabled={Boolean(operationId.current)}
              value={criteriaText}
              onChange={(event) => setCriteriaText(event.target.value)}
              rows={4}
              placeholder="每行填写一条最终确认的验收标准"
              className="mt-2 w-full resize-none rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.5)] px-4 py-3 text-[14px] text-white outline-none focus:border-[var(--ap-border-strong)]"
            />
          </div>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="min-w-0">
              <label htmlFor="task-budget" className="text-[13px] text-[var(--ap-muted)]">任务预算 (USDC)</label>
              <input
                id="task-budget"
                disabled={Boolean(operationId.current)}
                value={amount} onChange={(e) => setAmount(e.target.value.replace(/\D/g, ''))}
                className="mt-2 w-full rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.5)] px-4 py-3 text-[14px] text-white outline-none focus:border-[var(--ap-border-strong)]"
              />
            </div>
            <div className="min-w-0">
              <label htmlFor="task-deadline" className="text-[13px] text-[var(--ap-muted)]">期望交付时间</label>
              <input id="task-deadline" type="date" value={deadline} disabled={Boolean(operationId.current)} onChange={(event) => setDeadline(event.target.value)}
                className="mt-2 w-full rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.5)] px-4 py-3 text-[14px] text-white outline-none focus:border-[var(--ap-border-strong)] [color-scheme:dark]"
              />
            </div>
          </div>
        </Panel>

        <div className="space-y-6">
          <Panel strong className="p-6">
            <SectionTitle right={<Pill tone="cyan" dot>Engine 权威判定</Pill>}>不可变发布确认</SectionTitle>
            <div className="space-y-3 text-[14px]">
              <Row k="概览与正式预算" v={`${Number(amount || 0).toLocaleString()} USDC`} />
              <Row k="预计平台服务费" v={`${fee} USDC`} muted />
              <Row k="Agent 履约保证金" v="0 USDC" highlight="Agent 零履约金" />
              <div className="h-px bg-[var(--ap-border)]" />
              <Row k="本次操作" v="发布任务规格" big />
            </div>
            <CtaButton full icon={Lock} className="mt-5" onClick={submit} disabled={submitting || Boolean(publication)} busy={submitting}>{submitting ? '发布中…' : publication ? '已不可变发布' : '创建草稿并发布'}</CtaButton>
            <p className="mt-3 flex items-center gap-1.5 text-[12px] text-[var(--ap-muted)]">
              <Info size={13} /> UI 仅执行 Engine 返回的 allowed 操作，不自行推导发布资格
            </p>
            {error ? <div className="mt-4 space-y-3"><div role="alert" className="rounded-xl border border-rose-300/30 bg-rose-300/10 px-4 py-3 text-[13px] text-rose-100">{error}</div>{operationId.current ? <><p className="text-[12px] text-[var(--ap-muted)]">为保证幂等重试，当前输入已锁定。可原样重试，或放弃本次操作后修改。</p><GhostButton onClick={resetAttempt}>放弃本次操作并重新编辑</GhostButton></> : null}</div> : null}
            {publication ? <div role="status" className="mt-4 space-y-3 rounded-xl border border-emerald-300/30 bg-emerald-300/10 px-4 py-3 text-[12px] text-emerald-100"><div><div>任务 {publication.taskId} 已发布</div><div className="break-all">规格：{publication.specHash}</div><div className="break-all">验收：{publication.acceptanceHash}</div>{funding ? <div className="mt-2">托管：{funding.status}{funding.transactionHash ? ` · ${funding.transactionHash.slice(0, 12)}…` : ''}</div> : null}</div>{funding?.status === 'confirmed' ? <GhostButton onClick={() => navigate(`/publisher/recommendations?taskId=${encodeURIComponent(publication.taskId)}`, { state: { ...flowState, taskId: publication.taskId } satisfies PublisherFlowState })}>进入权威匹配</GhostButton> : <GhostButton onClick={() => void fundTask()} disabled={fundingBusy}>{fundingBusy ? '处理中…' : funding?.status === 'submitted' ? '同步托管确认' : '钱包托管任务资金'}</GhostButton>}</div> : null}
          </Panel>

          <Panel className="p-5 space-y-3">
            <InfoNote tone="green"><span className="inline-flex items-center gap-1.5"><ShieldCheck size={14} />角色、所有权、聚合版本与幂等性均由 Engine 校验</span></InfoNote>
            <InfoNote tone="cyan"><span className="inline-flex items-center gap-1.5"><FileCheck2 size={14} />发布会生成内容哈希绑定的规格与加权验收版本</span></InfoNote>
          </Panel>
        </div>
      </div>
    </Page>
  );
}

function Row({ k, v, muted, highlight, big }: { k: string; v: string; muted?: boolean; highlight?: string; big?: boolean }) {
  return (
    <div className="flex items-center justify-between">
      <span className="text-[var(--ap-muted)]">{k}</span>
      <span className="flex items-center gap-2">
        {highlight && <Pill tone="green">{highlight}</Pill>}
        <span className={big ? 'text-[18px] text-[var(--ap-cyan)]' : muted ? 'text-[var(--ap-text-2)]' : 'text-[var(--ap-text)]'}>{v}</span>
      </span>
    </div>
  );
}
