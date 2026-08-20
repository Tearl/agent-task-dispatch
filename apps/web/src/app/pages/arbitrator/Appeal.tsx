import { toast } from 'sonner';
import { RefreshCcw, Info, Ban, Users, CheckCircle2 } from 'lucide-react';
import { Page } from '../../components/AppShell';
import { PageHeader, Panel, SectionTitle, Pill, CtaButton, GhostButton, InfoNote } from '../../components/kit/primitives';

const APPEALS = [
  { id: 'APL-108', orig: 'ARB-742', task: 'TSK-1930 · 代码重构', reason: '需求方认为原裁决未充分考虑变更记录', eligible: true },
  { id: 'APL-104', orig: 'ARB-698', task: 'TSK-1877 · 数据分析', reason: 'Agent 方申诉证据被误判', eligible: false, note: '你是原裁决仲裁者，不可参与复核' },
];

export default function Appeal() {
  return (
    <Page>
      <PageHeader title="申诉与复核" subtitle="由新仲裁小组复核 · 不允许原仲裁者复核自己的裁决" />

      <InfoNote tone="amber">
        <span className="inline-flex items-center gap-1.5"><Info size={14} />申诉将组建全新的仲裁小组进行独立复核，系统自动排除原案仲裁者以保证中立。</span>
      </InfoNote>

      <div className="space-y-4">
        {APPEALS.map((a) => (
          <Panel key={a.id} className="p-5">
            <div className="flex items-start justify-between gap-4 flex-wrap">
              <div>
                <div className="flex items-center gap-2">
                  <span className="text-[15px] text-[var(--ap-text)]">{a.id}</span>
                  <Pill tone="violet">复核申请</Pill>
                  <span className="text-[12px] text-[var(--ap-muted)]">原案 {a.orig}</span>
                </div>
                <div className="mt-1.5 text-[13px] text-[var(--ap-text-2)]">{a.task}</div>
                <p className="mt-1 text-[13px] text-[var(--ap-muted)]">申诉理由：{a.reason}</p>
              </div>
              <div className="flex items-center gap-2">
                <Users size={15} className="text-[var(--ap-muted)]" />
                <span className="text-[12px] text-[var(--ap-muted)]">新仲裁小组 5 人</span>
              </div>
            </div>

            <div className="mt-4">
              {a.eligible ? (
                <div className="flex gap-2">
                  <CtaButton icon={CheckCircle2} onClick={() => toast.success('已加入复核小组')}>加入复核</CtaButton>
                  <GhostButton>查看原裁决</GhostButton>
                </div>
              ) : (
                <div className="flex items-center gap-2 rounded-xl border border-[rgba(251,113,133,0.25)] bg-[rgba(251,113,133,0.08)] px-4 py-2.5 text-[13px] text-[var(--ap-danger)]">
                  <Ban size={15} /> {a.note}
                </div>
              )}
            </div>
          </Panel>
        ))}
      </div>

      <Panel className="p-5 flex items-center gap-3">
        <RefreshCcw size={18} className="text-[var(--ap-cyan)]" />
        <span className="text-[13px] text-[var(--ap-text-2)]">复核结果为终审，将由合约按新小组密封投票自动执行。</span>
      </Panel>
    </Page>
  );
}

