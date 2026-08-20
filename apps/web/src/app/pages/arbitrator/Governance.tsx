import { useState } from 'react';
import { toast } from 'sonner';
import { Vote, Lock, FileText, Info, CheckCircle2 } from 'lucide-react';
import { Page } from '../../components/AppShell';
import { PageHeader, Panel, SectionTitle, Pill, GhostButton, InfoNote, Bar } from '../../components/kit/primitives';

interface Proposal { id: string; title: string; type: string; status: 'active' | 'sealed' | 'passed' | 'rejected'; forPct: number; ends: string; }
const PROPOSALS: Proposal[] = [
  { id: 'GOV-042', title: '将争议举证期从 48h 延长至 72h', type: '规则参数', status: 'active', forPct: 64, ends: '2 天后' },
  { id: 'GOV-041', title: '提高最低仲裁质押门槛至 4,000 YD', type: '资格参数', status: 'sealed', forPct: 0, ends: '密封中' },
  { id: 'GOV-039', title: '引入匹配洗牌随机种子公示机制', type: '匹配规则', status: 'passed', forPct: 78, ends: '已通过' },
  { id: 'GOV-036', title: '仲裁奖励下调 10%', type: '经济参数', status: 'rejected', forPct: 32, ends: '已否决' },
];

const TONE = { active: 'violet', sealed: 'cyan', passed: 'green', rejected: 'red' } as const;
const LABEL = { active: '投票中', sealed: '密封中', passed: '已通过', rejected: '已否决' };

export default function Governance() {
  const [voted, setVoted] = useState<Record<string, boolean>>({});
  return (
    <Page>
      <PageHeader title="社区治理" subtitle="提案、规则参数、密封投票与治理记录" />

      <InfoNote tone="cyan">
        <span className="inline-flex items-center gap-1.5"><Info size={14} />治理提案采用密封投票，到期统一揭晓；规则参数变更经通过后按版本化配置生效。</span>
      </InfoNote>

      <div className="space-y-4">
        {PROPOSALS.map((p) => (
          <Panel key={p.id} className="p-5">
            <div className="flex items-start justify-between gap-4 flex-wrap">
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <span className="text-[15px] text-[var(--ap-text)]">{p.title}</span>
                  <Pill tone={TONE[p.status]}>{LABEL[p.status]}</Pill>
                </div>
                <div className="mt-1 text-[12px] text-[var(--ap-muted)]">{p.id} · {p.type} · {p.ends}</div>
              </div>
              {p.status === 'active' && (
                <div className="flex gap-2">
                  {voted[p.id] ? (
                    <span className="inline-flex items-center gap-1.5 text-[13px] text-[var(--ap-success)]"><CheckCircle2 size={15} />已密封投票</span>
                  ) : (
                    <>
                      <GhostButton icon={Lock} onClick={() => { setVoted((v) => ({ ...v, [p.id]: true })); toast.success('赞成票已密封'); }}>赞成</GhostButton>
                      <GhostButton onClick={() => { setVoted((v) => ({ ...v, [p.id]: true })); toast.success('反对票已密封'); }}>反对</GhostButton>
                    </>
                  )}
                </div>
              )}
            </div>
            {(p.status === 'passed' || p.status === 'rejected' || p.status === 'active') && (
              <div className="mt-4">
                <div className="flex justify-between text-[12px] text-[var(--ap-muted)]">
                  <span>{p.status === 'active' ? '当前赞成（实时估算）' : '最终赞成率'}</span><span>{p.forPct}%</span>
                </div>
                <div className="mt-1.5"><Bar value={p.forPct} tone={p.forPct >= 50 ? '#34d399' : '#fb7185'} /></div>
              </div>
            )}
          </Panel>
        ))}
      </div>

      <Panel className="p-5 flex items-center justify-between">
        <span className="flex items-center gap-2 text-[13px] text-[var(--ap-text-2)]"><FileText size={16} className="text-[var(--ap-cyan)]" />所有治理记录只追加存证，可完整回溯</span>
        <GhostButton icon={Vote}>查看治理记录</GhostButton>
      </Panel>
    </Page>
  );
}

