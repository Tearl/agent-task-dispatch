import { toast } from 'sonner';
import { useNavigate } from 'react-router';
import { ShieldAlert, CheckCircle2, XCircle, Info } from 'lucide-react';
import { Page } from '../../components/AppShell';
import { PageHeader, Panel, Pill, CtaButton, GhostButton, InfoNote } from '../../components/kit/primitives';
import { CASES } from '../../lib/mock';

export default function PendingCases() {
  const nav = useNavigate();
  return (
    <Page>
      <PageHeader title="待处理案件" subtitle="系统分配 · 利益冲突检查 · 接受或申请回避" />

      <InfoNote tone="cyan">
        <span className="inline-flex items-center gap-1.5"><Info size={14} />案件由系统随机分配以保证公正。若与当事方存在利益关联，请主动申请回避。</span>
      </InfoNote>

      <div className="space-y-4">
        {CASES.filter((c) => ['evidence', 'voting'].includes(c.status)).map((c) => (
          <Panel key={c.id} className="p-5">
            <div className="flex items-start justify-between gap-4 flex-wrap">
              <div>
                <div className="flex items-center gap-2">
                  <span className="text-[15px] text-[var(--ap-text)]">{c.id}</span>
                  <Pill tone="green">系统分配</Pill>
                  <Pill tone="gray">{c.role}</Pill>
                </div>
                <div className="mt-1.5 text-[13px] text-[var(--ap-text-2)]">{c.task}</div>
                <p className="mt-1 text-[13px] text-[var(--ap-muted)]">{c.summary}</p>
              </div>
              <div className="text-right">
                <div className="text-[12px] text-[var(--ap-muted)]">冻结金额</div>
                <div className="text-[18px] text-white">{c.frozen.toLocaleString()} USDC</div>
              </div>
            </div>

            <div className="mt-4 flex items-center justify-between rounded-xl border border-[rgba(251,191,36,0.25)] bg-[rgba(251,191,36,0.08)] px-4 py-2.5">
              <span className="flex items-center gap-2 text-[13px] text-[var(--ap-warning)]"><ShieldAlert size={15} />利益冲突自检：未检测到与当事方的关联</span>
              <span className="text-[12px] text-[var(--ap-muted)]">投票截止 {c.deadline}</span>
            </div>

            <div className="mt-4 flex gap-2">
              <CtaButton icon={CheckCircle2} onClick={() => { toast.success('已接受案件'); nav('/arbitrator/review'); }}>接受并审理</CtaButton>
              <GhostButton icon={XCircle} onClick={() => toast.success('已申请回避，系统将重新分配')}>申请回避</GhostButton>
            </div>
          </Panel>
        ))}
      </div>
    </Page>
  );
}

