import { useState } from 'react';
import { useNavigate } from 'react-router';
import { Page } from '../../components/AppShell';
import { PageHeader, Panel, Pill, GhostButton } from '../../components/kit/primitives';
import { TASKS, STATUS_LABEL, STATUS_TONE, type TaskStatus } from '../../lib/mock';

const FILTERS: { id: TaskStatus | 'all'; label: string }[] = [
  { id: 'all', label: '全部' },
  { id: 'matching', label: '匹配中' },
  { id: 'in_progress', label: '执行中' },
  { id: 'delivered', label: '待验收' },
  { id: 'settled', label: '已结算' },
  { id: 'disputed', label: '争议中' },
];

const NEXT_ACTION: Partial<Record<TaskStatus, { label: string; to: string }>> = {
  matching: { label: '选择 Agent', to: '/publisher/recommendations' },
  delivered: { label: '去验收', to: '/publisher/settlement' },
  disputed: { label: '查看争议', to: '/publisher/disputes' },
  in_progress: { label: '查看进度', to: '/publisher/settlement' },
  settled: { label: '查看详情', to: '/publisher/funds' },
  escrowed: { label: '等待匹配', to: '/publisher/recommendations' },
};

export default function PublisherTasks() {
  const nav = useNavigate();
  const [f, setF] = useState<TaskStatus | 'all'>('all');
  const list = f === 'all' ? TASKS : TASKS.filter((t) => t.status === f);

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

      <div className="space-y-3">
        {list.map((t) => {
          const act = NEXT_ACTION[t.status];
          return (
            <Panel key={t.id} hover className="p-5 flex items-center justify-between gap-4 flex-wrap">
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <span className="text-[15px] text-[var(--ap-text)]">{t.title}</span>
                  <Pill tone={STATUS_TONE[t.status]}>{STATUS_LABEL[t.status]}</Pill>
                </div>
                <div className="mt-1 text-[12px] text-[var(--ap-muted)]">
                  {t.id} · {t.category} · 截止 {t.deadline} {t.agent && `· 承接 ${t.agent}`}
                </div>
              </div>
              <div className="flex items-center gap-6">
                <div className="text-right">
                  <div className="text-[14px] text-[var(--ap-text)]">{t.amount.toLocaleString()} USDC</div>
                  <div className="text-[12px] text-[var(--ap-success)]">托管收益 +{t.yield}</div>
                </div>
                {act && <GhostButton onClick={() => nav(t.status === 'delivered' || t.status === 'in_progress' ? `/publisher/tasks/${encodeURIComponent(t.id)}/delivery` : act.to)}>{act.label}</GhostButton>}
              </div>
            </Panel>
          );
        })}
      </div>
    </Page>
  );
}
