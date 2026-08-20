import { toast } from 'sonner';
import { ShieldCheck, Coins, Award, TrendingUp, Info, Lock } from 'lucide-react';
import { Page } from '../../components/AppShell';
import { PageHeader, StatCard, Panel, SectionTitle, Pill, CtaButton, GhostButton, InfoNote, Bar } from '../../components/kit/primitives';

export default function Staking() {
  return (
    <Page>
      <PageHeader title="质押、资格与奖励" subtitle="YD 治理质押、仲裁资格、贡献与奖励" />

      <InfoNote tone="green">
        <span className="inline-flex items-center gap-1.5"><Info size={14} />YD 为治理代币，质押用于获得仲裁资格与投票权重，并非 Agent 履约金，不参与任务担保。</span>
      </InfoNote>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard label="已质押 YD" value="5,000" unit="YD" icon={Lock} accent="#34d399" />
        <StatCard label="仲裁资格" value="有效" icon={ShieldCheck} accent="#22d3ee" hint="达最低门槛" />
        <StatCard label="累计奖励" value="842" unit="YD" icon={Award} delta={9} accent="#fbbf24" />
        <StatCard label="贡献分" value="1,260" icon={TrendingUp} delta={4} accent="#8b5cf6" />
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        <Panel strong className="p-6">
          <SectionTitle right={<Pill tone="green" dot>治理质押</Pill>}>质押管理</SectionTitle>
          <div className="space-y-3 text-[13px]">
            <div className="flex justify-between"><span className="text-[var(--ap-muted)]">当前质押</span><span className="text-[var(--ap-text)]">5,000 YD</span></div>
            <div className="flex justify-between"><span className="text-[var(--ap-muted)]">资格门槛</span><span className="text-[var(--ap-text)]">≥ 3,000 YD</span></div>
            <div><div className="flex justify-between text-[var(--ap-muted)]"><span>投票权重</span><span>1.0×</span></div><div className="mt-1.5"><Bar value={62} tone="#34d399" /></div></div>
          </div>
          <input placeholder="输入质押 / 赎回数量"
            className="mt-4 w-full rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.5)] px-4 py-3 text-[14px] text-white outline-none focus:border-[var(--ap-border-strong)]" />
          <div className="mt-3 flex gap-2">
            <CtaButton className="flex-1" icon={Lock} onClick={() => toast.success('已追加质押')}>追加质押</CtaButton>
            <GhostButton className="flex-1" onClick={() => toast.success('赎回申请已提交（冷静期 7 天）')}>赎回</GhostButton>
          </div>
        </Panel>

        <Panel className="p-6">
          <SectionTitle right={<GhostButton icon={Coins} onClick={() => toast.success('奖励已领取')}>领取奖励</GhostButton>}>贡献与奖励</SectionTitle>
          <div className="space-y-3">
            {[
              { t: '诚信裁决奖励', v: '+320 YD', d: '与多数正确裁决一致' },
              { t: '按时参与奖励', v: '+180 YD', d: '30 天全勤投票' },
              { t: '治理提案奖励', v: '+90 YD', d: '参与 3 项提案投票' },
            ].map((r) => (
              <div key={r.t} className="flex items-center justify-between rounded-xl border border-[var(--ap-border)] px-4 py-3">
                <div>
                  <div className="text-[14px] text-[var(--ap-text)]">{r.t}</div>
                  <div className="text-[12px] text-[var(--ap-muted)]">{r.d}</div>
                </div>
                <span className="text-[14px] text-[var(--ap-success)]">{r.v}</span>
              </div>
            ))}
          </div>
        </Panel>
      </div>
    </Page>
  );
}
