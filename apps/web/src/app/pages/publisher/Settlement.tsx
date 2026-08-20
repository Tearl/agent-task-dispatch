import { toast } from 'sonner';
import {
  FileText, Download, CheckCircle2, AlertTriangle, Link2, TrendingUp, ShieldCheck,
} from 'lucide-react';
import { Page } from '../../components/AppShell';
import { PageHeader, Panel, SectionTitle, Pill, CtaButton, GhostButton, InfoNote } from '../../components/kit/primitives';

const DELIVERABLES = [
  { name: '本地化译文_8语种.zip', size: '12.4 MB', hash: '0x8f3a…c21d' },
  { name: '术语一致性报告.pdf', size: '820 KB', hash: '0x21be…9a4f' },
  { name: '质检自评表.xlsx', size: '96 KB', hash: '0x7cd0…1e88' },
];

const CHAIN = [
  { t: '任务款托管', v: '640 USDC', time: '08-16 10:22', tx: '0xa1…f0' },
  { t: '托管期生息', v: '+3.1 USDC', time: '实时累计', tx: '—' },
  { t: '交付上链存证', v: 'CID 已记录', time: '08-21 09:10', tx: '0xb2…7c' },
];

export default function Settlement() {
  return (
    <Page>
      <PageHeader
        title="交付、验收与结算"
        subtitle="TSK-2041 · 产品说明书 8 语种本地化翻译"
        actions={<GhostButton icon={AlertTriangle}>发起争议</GhostButton>}
      />

      <div className="grid gap-6 lg:grid-cols-[1.5fr_1fr]">
        <div className="space-y-6">
          <Panel className="p-6">
            <SectionTitle right={<Pill tone="amber" dot>待验收</Pill>}>完整交付结果</SectionTitle>
            <div className="space-y-3">
              {DELIVERABLES.map((d) => (
                <div key={d.name} className="flex items-center justify-between rounded-xl border border-[var(--ap-border)] bg-[rgba(10,18,38,0.4)] px-4 py-3">
                  <div className="flex items-center gap-3">
                    <FileText size={18} className="text-[var(--ap-cyan)]" />
                    <div>
                      <div className="text-[14px] text-[var(--ap-text)]">{d.name}</div>
                      <div className="text-[12px] text-[var(--ap-muted)]">{d.size} · 内容哈希 {d.hash}</div>
                    </div>
                  </div>
                  <button className="grid h-9 w-9 place-items-center rounded-lg border border-[var(--ap-border)] text-[var(--ap-text-2)] hover:border-[var(--ap-border-strong)]">
                    <Download size={16} />
                  </button>
                </div>
              ))}
            </div>
          </Panel>

          <Panel className="p-6">
            <SectionTitle right={<Pill tone="cyan" dot>链上资金</Pill>}>资金与存证流水</SectionTitle>
            <div className="space-y-2">
              {CHAIN.map((c) => (
                <div key={c.t} className="flex items-center justify-between rounded-xl px-4 py-3" style={{ background: 'rgba(10,18,38,0.4)' }}>
                  <div className="flex items-center gap-3">
                    <Link2 size={15} className="text-[var(--ap-muted)]" />
                    <span className="text-[13px] text-[var(--ap-text-2)]">{c.t}</span>
                  </div>
                  <div className="flex items-center gap-4 text-[13px]">
                    <span className="text-[var(--ap-text)]">{c.v}</span>
                    <span className="text-[var(--ap-muted)]">{c.time}</span>
                    <span className="text-[var(--ap-cyan)]">{c.tx}</span>
                  </div>
                </div>
              ))}
            </div>
          </Panel>
        </div>

        <div className="space-y-6">
          <Panel strong className="p-6">
            <SectionTitle>结算明细</SectionTitle>
            <div className="space-y-3 text-[14px]">
              <Row k="托管本金" v="640 USDC" />
              <Row k="结算前收益（归你）" v="+3.1 USDC" tone="green" icon />
              <Row k="平台服务费 (2%)" v="-12.8 USDC" muted />
              <div className="h-px bg-[var(--ap-border)]" />
              <Row k="应支付 Agent" v="627.2 USDC" big />
              <Row k="退回你钱包（含收益）" v="3.1 USDC" tone="green" />
            </div>
            <CtaButton full icon={CheckCircle2} className="mt-5" onClick={() => toast.success('已验收，资金按结算规则链上划转')}>
              确认验收并结算
            </CtaButton>
            <p className="mt-3 text-center text-[12px] text-[var(--ap-muted)]">结算后 Agent 收入可立即提取或自愿生息</p>
          </Panel>

          <InfoNote tone="green">
            <span className="inline-flex items-center gap-1.5"><ShieldCheck size={14} />若结果不符预期，可在验收期内提交证据发起争议，任务款自动冻结。</span>
          </InfoNote>
        </div>
      </div>
    </Page>
  );
}

function Row({ k, v, muted, big, tone, icon }: { k: string; v: string; muted?: boolean; big?: boolean; tone?: 'green'; icon?: boolean }) {
  const color = tone === 'green' ? 'var(--ap-success)' : big ? 'var(--ap-cyan)' : muted ? 'var(--ap-text-2)' : 'var(--ap-text)';
  return (
    <div className="flex items-center justify-between">
      <span className="text-[var(--ap-muted)]">{k}</span>
      <span className="inline-flex items-center gap-1" style={{ color, fontSize: big ? 18 : 14 }}>
        {icon && <TrendingUp size={13} />}{v}
      </span>
    </div>
  );
}
