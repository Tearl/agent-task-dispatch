import { useState } from 'react';
import { toast } from 'sonner';
import {
  Snowflake, FileLock2, Lock, ShieldCheck, Scale, Info, EyeOff,
} from 'lucide-react';
import { Page } from '../../components/AppShell';
import { PageHeader, Panel, SectionTitle, Pill, CtaButton, GhostButton, InfoNote, Bar } from '../../components/kit/primitives';

type Vote = 'publisher' | 'agent' | 'split' | null;

export default function CaseReview() {
  const [vote, setVote] = useState<Vote>(null);
  const [sealed, setSealed] = useState(false);

  const submit = () => {
    if (!vote) { toast.error('请先选择裁决意见'); return; }
    setSealed(true);
    toast.success('已提交密封投票，到期前不可查看结果');
  };

  return (
    <Page>
      <PageHeader title="仲裁案件审理" subtitle="ARB-771 · TSK-2012 市场消费者调研报告" actions={<Pill tone="violet" dot>密封投票阶段</Pill>} />

      <InfoNote tone="amber">
        <span className="inline-flex items-center gap-1.5"><Info size={14} />单个仲裁者只能投票，不能直接划转争议资金；裁决按仲裁小组密封投票结果由合约自动执行。</span>
      </InfoNote>

      <div className="grid gap-6 lg:grid-cols-[1.5fr_1fr]">
        <div className="space-y-6">
          <Panel className="p-6">
            <SectionTitle right={<Pill tone="cyan"><span className="inline-flex items-center gap-1"><EyeOff size={12} />已脱敏</span></Pill>}>脱敏证据</SectionTitle>
            <div className="space-y-3">
              {[
                { side: '需求方', files: ['验收标准_原始约定.pdf', '样本数据核对表.xlsx'] },
                { side: 'Agent 方', files: ['交付报告_全文.pdf', '数据来源与方法说明.md'] },
              ].map((b) => (
                <div key={b.side} className="rounded-xl border border-[var(--ap-border)] bg-[rgba(10,18,38,0.4)] p-4">
                  <div className="text-[13px] text-[var(--ap-text-2)]">{b.side}提交</div>
                  <div className="mt-2 space-y-2">
                    {b.files.map((f) => (
                      <div key={f} className="flex items-center justify-between text-[13px] text-[var(--ap-muted)]">
                        <span className="inline-flex items-center gap-2"><FileLock2 size={14} className="text-[var(--ap-cyan)]" />{f}</span>
                        <span className="font-mono text-[12px]">当事方身份已隐藏</span>
                      </div>
                    ))}
                  </div>
                </div>
              ))}
            </div>
          </Panel>

          <Panel className="p-6">
            <SectionTitle>裁决意见（密封）</SectionTitle>
            <div className="grid gap-3 sm:grid-cols-3">
              {[
                { id: 'publisher', label: '支持需求方', desc: '全额退款' },
                { id: 'split', label: '按完成度分账', desc: '部分结算' },
                { id: 'agent', label: '支持 Agent', desc: '全额结算' },
              ].map((o) => (
                <button key={o.id} disabled={sealed} onClick={() => setVote(o.id as Vote)}
                  className="rounded-xl border p-4 text-left transition-colors disabled:opacity-60"
                  style={{
                    borderColor: vote === o.id ? 'var(--ap-border-strong)' : 'var(--ap-border)',
                    background: vote === o.id ? 'var(--ap-cyan-soft)' : 'transparent',
                  }}>
                  <Scale size={17} className="text-[var(--ap-cyan)]" />
                  <div className="mt-2 text-[14px] text-[var(--ap-text)]">{o.label}</div>
                  <div className="text-[12px] text-[var(--ap-muted)]">{o.desc}</div>
                </button>
              ))}
            </div>
            {vote === 'split' && (
              <div className="mt-4">
                <div className="flex justify-between text-[13px] text-[var(--ap-muted)]"><span>建议结算比例</span><span>70%</span></div>
                <div className="mt-2"><Bar value={70} /></div>
              </div>
            )}
            {!sealed ? (
              <CtaButton full icon={Lock} className="mt-5" onClick={submit}>密封提交投票</CtaButton>
            ) : (
              <div className="mt-5 flex items-center justify-center gap-2 rounded-xl border border-[rgba(52,211,153,0.3)] bg-[rgba(52,211,153,0.08)] py-3 text-[14px] text-[var(--ap-success)]">
                <ShieldCheck size={16} /> 已密封提交，等待到期揭晓
              </div>
            )}
          </Panel>
        </div>

        <div className="space-y-6">
          <Panel strong className="p-6">
            <div className="flex items-center gap-2 text-[13px] text-[var(--ap-danger)]"><Snowflake size={15} />冻结中争议资金</div>
            <div className="mt-2 text-[28px] text-white">1,500 <span className="text-[14px] text-[var(--ap-muted)]">USDC</span></div>
            <p className="mt-2 text-[12px] text-[var(--ap-muted)]">资金锁定在托管合约，任何仲裁者都无法直接划转，只能通过投票影响裁决。</p>
          </Panel>

          <Panel className="p-6">
            <SectionTitle>治理质押（YD）</SectionTitle>
            <div className="space-y-2 text-[13px]">
              <div className="flex justify-between"><span className="text-[var(--ap-muted)]">已质押</span><span className="text-[var(--ap-text)]">5,000 YD</span></div>
              <div className="flex justify-between"><span className="text-[var(--ap-muted)]">本案投票权重</span><span className="text-[var(--ap-text)]">1.0×</span></div>
              <div className="flex justify-between"><span className="text-[var(--ap-muted)]">诚信裁决奖励</span><span className="text-[var(--ap-success)]">+参与奖励</span></div>
            </div>
            <InfoNote tone="green" ><span className="inline-flex items-center gap-1.5"><Info size={12} />YD 治理质押用于资格与投票，不作 Agent 履约金。</span></InfoNote>
          </Panel>

          <GhostButton className="w-full">请求案件补充材料</GhostButton>
        </div>
      </div>
    </Page>
  );
}

