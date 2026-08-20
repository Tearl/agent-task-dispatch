import { toast } from 'sonner';
import { Upload, Info, Snowflake, MessageSquareWarning, Circle, CheckCircle2 } from 'lucide-react';
import { Page } from '../../components/AppShell';
import { PageHeader, Panel, SectionTitle, Pill, CtaButton, InfoNote } from '../../components/kit/primitives';

const STEPS = ['提交争议', '举证期', '仲裁审理', '密封投票', '裁决执行'];
const CURRENT = 3;

export default function PublisherDisputes() {
  return (
    <Page>
      <PageHeader title="争议处理" subtitle="提交证据并查看进度 · 任务发布方没有仲裁投票权" />

      <InfoNote tone="amber">
        <span className="inline-flex items-center gap-1.5"><Info size={14} />你可提交证据并跟踪进度，但不参与仲裁投票；争议期间任务款保持冻结，裁决由仲裁小组作出。</span>
      </InfoNote>

      <Panel className="p-6">
        <SectionTitle right={<Pill tone="red" dot>ARB-771 · 争议中</Pill>}>TSK-2012 · 市场消费者调研报告</SectionTitle>

        <div className="flex items-center gap-2 mb-6 mt-2 flex-wrap">
          {STEPS.map((s, i) => (
            <div key={s} className="flex items-center gap-2">
              <span className="flex items-center gap-1.5 text-[13px]" style={{ color: i <= CURRENT ? '#67e8f9' : 'var(--ap-muted)' }}>
                {i < CURRENT ? <CheckCircle2 size={16} /> : <Circle size={16} />} {s}
              </span>
              {i < STEPS.length - 1 && <span className="h-px w-8" style={{ background: i < CURRENT ? '#22d3ee' : 'var(--ap-border)' }} />}
            </div>
          ))}
        </div>

        <div className="grid gap-6 lg:grid-cols-[1.4fr_1fr]">
          <div className="space-y-4">
            <div className="rounded-xl border border-[var(--ap-border)] bg-[rgba(10,18,38,0.4)] p-4">
              <div className="text-[13px] text-[var(--ap-muted)]">争议摘要</div>
              <p className="mt-1.5 text-[14px] text-[var(--ap-text-2)]">交付报告在样本量与结论可靠性上未满足验收标准第 3 条，要求部分退款。</p>
            </div>

            <div>
              <div className="text-[13px] text-[var(--ap-muted)] mb-2">我方提交的证据</div>
              <div className="space-y-2">
                {['验收标准_原始约定.pdf', '交付报告_批注版.pdf', '样本数据核对表.xlsx'].map((f) => (
                  <div key={f} className="flex items-center justify-between rounded-lg border border-[var(--ap-border)] px-3 py-2.5 text-[13px] text-[var(--ap-text-2)]">
                    <span>{f}</span><Pill tone="cyan">已上链存证</Pill>
                  </div>
                ))}
              </div>
              <button
                onClick={() => toast.success('证据已提交并加密存证')}
                className="mt-3 flex w-full items-center justify-center gap-2 rounded-xl border border-dashed border-[var(--ap-border-strong)] py-3 text-[13px] text-[var(--ap-text-2)] hover:bg-[rgba(34,211,238,0.05)]"
              >
                <Upload size={15} /> 补充上传证据材料
              </button>
            </div>
          </div>

          <div className="space-y-4">
            <Panel strong className="p-5">
              <div className="flex items-center gap-2 text-[13px] text-[var(--ap-danger)]">
                <Snowflake size={15} /> 冻结中任务款
              </div>
              <div className="mt-2 text-[26px] text-white">1,500 <span className="text-[14px] text-[var(--ap-muted)]">USDC</span></div>
              <p className="mt-2 text-[12px] text-[var(--ap-muted)]">裁决作出前资金锁定于托管合约，任何单一角色都无法直接划转。</p>
            </Panel>
            <div className="rounded-xl border border-[var(--ap-border)] p-4">
              <div className="text-[13px] text-[var(--ap-muted)]">当前进度</div>
              <p className="mt-1.5 text-[14px] text-[var(--ap-text-2)]">已组建 5 人仲裁小组，进入密封投票，预计 12 小时后揭晓。</p>
            </div>
            <CtaButton full icon={MessageSquareWarning}>与仲裁小组沟通（脱敏）</CtaButton>
          </div>
        </div>
      </Panel>
    </Page>
  );
}
