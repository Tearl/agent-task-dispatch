import { useState } from 'react';
import { useLocation, useNavigate } from 'react-router';
import { toast } from 'sonner';
import { ShieldCheck, Lock, Info, FileCheck2 } from 'lucide-react';
import { Page } from '../../components/AppShell';
import { PageHeader, Panel, SectionTitle, CtaButton, InfoNote, Pill } from '../../components/kit/primitives';
import { AGENTS } from '../../lib/mock';
import type { PublisherFlowState } from '../../lib/publisher-flow';

const CATEGORIES = ['数据分析', '翻译', '图像生成', '代码开发', '市场研究', '智能审计'];

export default function PublishTask() {
  const nav = useNavigate();
  const location = useLocation();
  const flowState = (location.state ?? {}) as PublisherFlowState;
  const analysis = flowState.analysis;
  const selectedAgent = AGENTS.find((agent) => agent.id === flowState.selectedAgentId);
  const [title, setTitle] = useState(analysis?.title ?? '');
  const [cat, setCat] = useState(analysis?.category ?? '数据分析');
  const [amount, setAmount] = useState(String(Math.max(analysis?.budget ?? 0, selectedAgent?.price ?? 1200)));
  const [desc, setDesc] = useState(
    analysis
      ? `${analysis.summary}\n\n交付物：\n${analysis.deliverables.map((item) => `- ${item}`).join('\n')}\n\n验收标准：\n${analysis.acceptanceCriteria.map((item) => `- ${item}`).join('\n')}`
      : '',
  );
  const fee = Math.round(Number(amount || 0) * 0.02);

  const submit = () => {
    toast.success('任务款已单边托管，任务已进入执行准备阶段');
    nav('/publisher/tasks');
  };

  return (
    <Page>
      <PageHeader title="确认任务与资金托管" subtitle="确认 AI 分析结果与所选 Agent 后，由任务方单边托管任务款" />

      <div className="grid gap-6 lg:grid-cols-[1.5fr_1fr]">
        <Panel className="p-6 space-y-5">
          <SectionTitle>任务信息</SectionTitle>
          {selectedAgent ? (
            <div className="flex items-center justify-between rounded-xl border border-[var(--ap-border-strong)] bg-[var(--ap-cyan-soft)] px-4 py-3">
              <div>
                <div className="text-[11px] text-[var(--ap-muted)]">已选择推荐 Agent</div>
                <div className="mt-1 text-[14px] text-[var(--ap-cyan)]">{selectedAgent.name} · 匹配分 {selectedAgent.match}</div>
              </div>
              <Pill tone="green">等待托管确认</Pill>
            </div>
          ) : null}
          <div>
            <label className="text-[13px] text-[var(--ap-muted)]">任务标题</label>
            <input
              value={title} onChange={(e) => setTitle(e.target.value)}
              placeholder="例如：跨境电商竞品数据抓取与结构化"
              className="mt-2 w-full rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.5)] px-4 py-3 text-[14px] text-white outline-none focus:border-[var(--ap-border-strong)]"
            />
          </div>
          <div>
            <label className="text-[13px] text-[var(--ap-muted)]">任务分类</label>
            <div className="mt-2 flex flex-wrap gap-2">
              {CATEGORIES.map((c) => (
                <button
                  key={c} onClick={() => setCat(c)}
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
            <label className="text-[13px] text-[var(--ap-muted)]">需求描述与验收标准</label>
            <textarea
              value={desc} onChange={(e) => setDesc(e.target.value)} rows={5}
              placeholder="描述交付物、格式、验收标准与截止时间…"
              className="mt-2 w-full resize-none rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.5)] px-4 py-3 text-[14px] text-white outline-none focus:border-[var(--ap-border-strong)]"
            />
          </div>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="min-w-0">
              <label className="text-[13px] text-[var(--ap-muted)]">任务预算 (USDC)</label>
              <input
                value={amount} onChange={(e) => setAmount(e.target.value.replace(/\D/g, ''))}
                className="mt-2 w-full rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.5)] px-4 py-3 text-[14px] text-white outline-none focus:border-[var(--ap-border-strong)]"
              />
            </div>
            <div className="min-w-0">
              <label className="text-[13px] text-[var(--ap-muted)]">期望交付时间</label>
              <input type="date"
                className="mt-2 w-full rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.5)] px-4 py-3 text-[14px] text-white outline-none focus:border-[var(--ap-border-strong)] [color-scheme:dark]"
              />
            </div>
          </div>
        </Panel>

        <div className="space-y-6">
          <Panel strong className="p-6">
            <SectionTitle right={<Pill tone="cyan" dot>链上托管</Pill>}>资金托管确认</SectionTitle>
            <div className="space-y-3 text-[14px]">
              <Row k="托管本金（单边全额）" v={`${Number(amount || 0).toLocaleString()} USDC`} />
              <Row k="平台服务费（结算时收取）" v={`${fee} USDC`} muted />
              <Row k="Agent 履约保证金" v="0 USDC" highlight="Agent 零履约金" />
              <div className="h-px bg-[var(--ap-border)]" />
              <Row k="需预存至托管合约" v={`${Number(amount || 0).toLocaleString()} USDC`} big />
            </div>
            <CtaButton full icon={Lock} className="mt-5" onClick={submit}>签名并托管，启动任务</CtaButton>
            <p className="mt-3 flex items-center gap-1.5 text-[12px] text-[var(--ap-muted)]">
              <Info size={13} /> 结算前收益归你所有，取消匹配可原路退款
            </p>
          </Panel>

          <Panel className="p-5 space-y-3">
            <InfoNote tone="green"><span className="inline-flex items-center gap-1.5"><ShieldCheck size={14} />真实合约托管，资金全程链上可追踪</span></InfoNote>
            <InfoNote tone="cyan"><span className="inline-flex items-center gap-1.5"><FileCheck2 size={14} />托管确认后锁定任务范围与所选 Agent，进入执行准备</span></InfoNote>
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
