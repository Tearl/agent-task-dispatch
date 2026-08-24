import { useState } from 'react';
import { useNavigate } from 'react-router';
import { Page } from '../../components/AppShell';
import { PageHeader, Panel, Pill, GhostButton } from '../../components/kit/primitives';
import { readWorkspaceTasks } from '../../lib/platform-api';
import { statusLabel, statusTone, taskAmount } from '../../lib/task-presentation';
import { useFinanceView } from '../../lib/use-finance-view';

const FILTERS = [
  { id: 'all', label: '全部' },
  { id: 'matching', label: '匹配中' },
  { id: 'formal_generating', label: '执行中' },
  { id: 'formal_review', label: '待验收' },
  { id: 'settled', label: '已结算' },
  { id: 'disputed', label: '争议中' },
];

const NEXT_ACTION: Record<string, { label: string; to: string } | undefined> = {
  matching: { label: '选择 Agent', to: '/publisher/recommendations' },
  overview_generating: { label: '查看概览', to: '/publisher/recommendations' },
  awaiting_selection: { label: '选择 Agent', to: '/publisher/recommendations' },
  formal_review: { label: '去验收', to: '/publisher/settlement' },
  disputed: { label: '查看争议', to: '/publisher/disputes' },
  formal_generating: { label: '查看进度', to: '/publisher/settlement' },
  settled: { label: '查看详情', to: '/publisher/funds' },
  escrowed: { label: '等待匹配', to: '/publisher/recommendations' },
};

export default function PublisherTasks() {
  const nav = useNavigate();
  const [f, setF] = useState('all');
  const { value, error, loading, reload } = useFinanceView(readWorkspaceTasks);
  const tasks = value?.tasks ?? [];
  const list = f === 'all' ? tasks : tasks.filter((t) => t.status === f);

  return (
    <Page>
      <PageHeader title="我的任务" subtitle="仅显示本人发布的任务，按状态筛选并给出对应下一步操作" />

      <div className="flex flex-wrap gap-2">
        {FILTERS.map((x) => (
          <button key={x.id} onClick={() => setF(x.id)}
            className="rounded-full border px-3.5 py-1.5 text-[13px] transition-colors"
            style={{
              borderColor: f === x.id ? 'var(--ap-border-strong)' : 'var(--ap-border)',
              background: f === x.id ? 'var(--ap-cyan-soft)' : 'transparent',
              color: f === x.id ? '#a5f3fc' : 'var(--ap-text-2)',
            }}>{x.label}</button>
        ))}
      </div>

      {loading ? <div role="status" className="py-8 text-center text-[var(--ap-muted)]">正在读取任务…</div> : null}
      {error ? <Panel className="p-4 text-rose-200"><span role="alert">{error}</span><div className="mt-2"><GhostButton onClick={reload}>重试</GhostButton></div></Panel> : null}

      <div className="space-y-3">
        {list.map((t) => {
          const act = NEXT_ACTION[t.status];
          return (
            <Panel key={t.id} hover className="p-5 flex items-center justify-between gap-4 flex-wrap">
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <span className="text-[15px] text-[var(--ap-text)]">{t.title}</span>
                  <Pill tone={statusTone(t.status)}>{statusLabel(t.status)}</Pill>
                </div>
                <div className="mt-1 text-[12px] text-[var(--ap-muted)]">
                  {t.id} · {t.category} · 截止 {new Date(t.deadline).toLocaleDateString()}
                </div>
              </div>
              <div className="flex items-center gap-6">
                <div className="text-right">
                  <div className="text-[14px] text-[var(--ap-text)]">{taskAmount(t)} 最小单位</div>
                  <div className="text-[12px] text-[var(--ap-muted)]">概览 {t.overviewBudget} · 正式 {t.formalBudget}</div>
                </div>
                {act && <GhostButton onClick={() => nav(act.to === '/publisher/recommendations' ? `${act.to}?taskId=${encodeURIComponent(t.id)}` : ['formal_review','formal_generating'].includes(t.status) ? `/publisher/tasks/${encodeURIComponent(t.id)}/delivery` : act.to)}>{act.label}</GhostButton>}
              </div>
            </Panel>
          );
        })}
      </div>
    </Page>
  );
}
