import { useState } from 'react';
import { Search, Filter, Info, TrendingUp } from 'lucide-react';
import { Page } from '../../components/AppShell';
import { PageHeader, Panel, Pill, GhostButton, Bar, InfoNote } from '../../components/kit/primitives';
import { readMarketplaceAgents } from '../../lib/platform-api';
import { useFinanceView } from '../../lib/use-finance-view';

const CATS = ['全部', '数据分析', '翻译', '图像生成', '代码开发', '市场研究', '智能审计'];
export default function Marketplace() {
  const [cat, setCat] = useState('全部');
  const [query,setQuery]=useState('');
  const {value,error,loading,reload}=useFinanceView(readMarketplaceAgents);
  const all = value?.marketplace ?? [];
  const list = cat === '全部' ? all : all.filter((a) => a.category === cat);
  const visible=list.filter((agent)=>`${agent.name} ${agent.category} ${agent.capabilities} ${agent.tags.join(' ')}`.toLowerCase().includes(query.toLowerCase()));

  return (
    <Page>
      <PageHeader title="Agent 大厅" subtitle="发现与筛选平台上的专业 Agent" />

      <InfoNote tone="cyan">
        <span className="inline-flex items-center gap-1.5"><Info size={14} />大厅仅用于发现与了解 Agent，不允许直接指派、竞价或抢单；请通过发布任务由系统智能匹配。</span>
      </InfoNote>

      <Panel className="p-4 flex items-center gap-3 flex-wrap">
        <div className="flex flex-1 items-center gap-2 rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.5)] px-3 py-2 min-w-[240px]">
          <Search size={16} className="text-[var(--ap-muted)]" />
          <input value={query} onChange={(event)=>setQuery(event.target.value)} placeholder="搜索 Agent 名称、能力或分类…" className="w-full bg-transparent text-[14px] text-white outline-none placeholder:text-[var(--ap-muted)]" />
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

      {loading?<div role="status" className="py-8 text-center text-[var(--ap-muted)]">正在读取 Agent 目录…</div>:null}
      {error?<Panel className="p-4 text-rose-200"><span role="alert">{error}</span><div className="mt-2"><GhostButton onClick={reload}>重试</GhostButton></div></Panel>:null}

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {visible.map((a) => (
          <Panel key={a.id} hover className="p-5">
            <div className="flex items-center gap-3">
              <span className="grid h-11 w-11 place-items-center rounded-xl bg-gradient-to-br from-[#8b5cf6] to-[#22d3ee] text-[#04121c]">{a.name[0]}</span>
              <div className="flex-1">
                <div className="flex items-center gap-2">
                  <span className="text-[15px] text-[var(--ap-text)]">{a.name}</span>
                  <Pill tone="violet">{a.category}</Pill>
                </div>
                <div className="mt-0.5 text-[12px] text-[var(--ap-muted)]">{a.status} · {a.health} · 容量 {a.activeCapacity}/{a.maxConcurrency}</div>
              </div>
            </div>
            <p className="mt-3 text-[13px] text-[var(--ap-text-2)]">{a.authorBio || a.capabilities}</p>
            <div className="mt-4 flex items-center justify-between text-[12px] text-[var(--ap-muted)]">
              <span className="inline-flex items-center gap-1"><TrendingUp size={12} />预计 {Math.ceil(a.estimatedDurationSeconds/60)} 分钟</span>
              <span className="text-[var(--ap-cyan)]">概览价 {a.overviewPrice || '未发布'}</span>
            </div>
            <div className="mt-2"><Bar value={a.health==='healthy'?100:a.health==='degraded'?60:10} tone="#8b5cf6" /></div>
          </Panel>
        ))}
      </div>
    </Page>
  );
}
