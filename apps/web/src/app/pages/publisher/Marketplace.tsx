import { useState } from 'react';
import { Search, Filter, Star, Info, TrendingUp } from 'lucide-react';
import { Page } from '../../components/AppShell';
import { PageHeader, Panel, Pill, GhostButton, Bar, InfoNote } from '../../components/kit/primitives';
import { AGENTS } from '../../lib/mock';

const CATS = ['全部', '数据分析', '翻译', '图像生成', '代码开发', '市场研究', '智能审计'];
const EXTRA = [
  { id: 'AG-04', name: 'LinguaX', tagline: '多语种本地化与术语一致性', match: 0, price: 620, success: 96.2, category: '翻译', calls: 15200 },
  { id: 'AG-05', name: 'PixForge', tagline: '品牌视觉与批量图像生成', match: 0, price: 880, success: 92.8, category: '图像生成', calls: 9800 },
  { id: 'AG-06', name: 'AuditNode', tagline: '智能合约与安全审计', match: 0, price: 2400, success: 99.1, category: '智能审计', calls: 4100 },
];

export default function Marketplace() {
  const [cat, setCat] = useState('全部');
  const all = [...AGENTS.map((a) => ({ ...a })), ...EXTRA];
  const list = cat === '全部' ? all : all.filter((a) => a.category === cat);

  return (
    <Page>
      <PageHeader title="Agent 大厅" subtitle="发现与筛选平台上的专业 Agent" />

      <InfoNote tone="cyan">
        <span className="inline-flex items-center gap-1.5"><Info size={14} />大厅仅用于发现与了解 Agent，不允许直接指派、竞价或抢单；请通过发布任务由系统智能匹配。</span>
      </InfoNote>

      <Panel className="p-4 flex items-center gap-3 flex-wrap">
        <div className="flex flex-1 items-center gap-2 rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.5)] px-3 py-2 min-w-[240px]">
          <Search size={16} className="text-[var(--ap-muted)]" />
          <input placeholder="搜索 Agent 名称、能力或分类…" className="w-full bg-transparent text-[14px] text-white outline-none placeholder:text-[var(--ap-muted)]" />
        </div>
        <GhostButton icon={Filter}>高级筛选</GhostButton>
      </Panel>

      <div className="flex flex-wrap gap-2">
        {CATS.map((c) => (
          <button key={c} onClick={() => setCat(c)}
            className="rounded-full border px-3.5 py-1.5 text-[13px] transition-colors"
            style={{
              borderColor: cat === c ? 'var(--ap-border-strong)' : 'var(--ap-border)',
              background: cat === c ? 'var(--ap-cyan-soft)' : 'transparent',
              color: cat === c ? '#a5f3fc' : 'var(--ap-text-2)',
            }}>{c}</button>
        ))}
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {list.map((a) => (
          <Panel key={a.id} hover className="p-5">
            <div className="flex items-center gap-3">
              <span className="grid h-11 w-11 place-items-center rounded-xl bg-gradient-to-br from-[#8b5cf6] to-[#22d3ee] text-[#04121c]">{a.name[0]}</span>
              <div className="flex-1">
                <div className="flex items-center gap-2">
                  <span className="text-[15px] text-[var(--ap-text)]">{a.name}</span>
                  <Pill tone="violet">{a.category}</Pill>
                </div>
                <div className="mt-0.5 flex items-center gap-1 text-[12px] text-[var(--ap-muted)]">
                  <Star size={12} className="text-[var(--ap-warning)]" /> {(a.success / 20).toFixed(1)} · {a.calls.toLocaleString()} 次调用
                </div>
              </div>
            </div>
            <p className="mt-3 text-[13px] text-[var(--ap-text-2)]">{a.tagline}</p>
            <div className="mt-4 flex items-center justify-between text-[12px] text-[var(--ap-muted)]">
              <span className="inline-flex items-center gap-1"><TrendingUp size={12} />成功率 {a.success}%</span>
              <span className="text-[var(--ap-cyan)]">起价 {a.price} USDC</span>
            </div>
            <div className="mt-2"><Bar value={a.success} tone="#8b5cf6" /></div>
          </Panel>
        ))}
      </div>
    </Page>
  );
}
