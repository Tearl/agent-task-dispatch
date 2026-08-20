import { useNavigate } from 'react-router';
import { Gavel, Clock, ShieldCheck, Vote, ArrowRight, Timer } from 'lucide-react';
import { Page } from '../../components/AppShell';
import { PageHeader, StatCard, Panel, SectionTitle, Pill } from '../../components/kit/primitives';
import { CASES } from '../../lib/mock';

const CASE_STATE: Record<string, { label: string; tone: 'amber' | 'violet' | 'cyan' | 'green' }> = {
  evidence: { label: '举证期', tone: 'amber' },
  voting: { label: '投票中', tone: 'violet' },
  sealed: { label: '已封存', tone: 'cyan' },
  ruled: { label: '已裁决', tone: 'green' },
  appeal: { label: '复核中', tone: 'amber' },
};

export default function ArbDashboard() {
  const nav = useNavigate();
  return (
    <Page>
      <PageHeader title="仲裁工作台" subtitle="待办、案件状态、投票截止与资格概览" />

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard label="待处理案件" value="4" unit="件" icon={Gavel} accent="#34d399" />
        <StatCard label="即将截止投票" value="1" unit="件" icon={Timer} accent="#fbbf24" hint="12:24 后" />
        <StatCard label="仲裁资格" value="有效" icon={ShieldCheck} accent="#22d3ee" hint="质押 5,000 YD" />
        <StatCard label="累计参与投票" value="126" unit="次" icon={Vote} delta={5} accent="#8b5cf6" />
      </div>

      <Panel className="p-5">
        <SectionTitle right={<button onClick={() => nav('/arbitrator/cases')} className="text-[12px] text-[var(--ap-cyan)]">全部案件</button>}>我的案件</SectionTitle>
        <div className="space-y-3">
          {CASES.map((c) => {
            const st = CASE_STATE[c.status];
            return (
              <button key={c.id} onClick={() => nav('/arbitrator/review')}
                className="ap-hoverable flex w-full items-center justify-between rounded-xl border border-[var(--ap-border)] bg-[rgba(10,18,38,0.4)] px-4 py-3 text-left">
                <div>
                  <div className="flex items-center gap-2">
                    <span className="text-[14px] text-[var(--ap-text)]">{c.id}</span>
                    <Pill tone={st.tone}>{st.label}</Pill>
                  </div>
                  <div className="mt-1 text-[12px] text-[var(--ap-muted)]">{c.task} · {c.summary}</div>
                </div>
                <div className="flex items-center gap-4">
                  <span className="flex items-center gap-1 text-[13px] text-[var(--ap-text-2)]"><Clock size={13} />{c.deadline}</span>
                  <ArrowRight size={15} className="text-[var(--ap-muted)]" />
                </div>
              </button>
            );
          })}
        </div>
      </Panel>
    </Page>
  );
}
