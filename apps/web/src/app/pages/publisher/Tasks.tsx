import { useRef, useState } from 'react';
import { useNavigate } from 'react-router';
import { Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import { Page } from '../../components/AppShell';
import { PageHeader, Panel, Pill, GhostButton } from '../../components/kit/primitives';
import { readWorkspaceTasks, requestTaskDeletion, submitTaskRefundTransaction, type WalletProvider } from '../../lib/platform-api';
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
  pending_escrow: { label: '托管资金', to: '/publisher/funding' },
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
	const [confirming, setConfirming] = useState<string | null>(null);
	const [deleting, setDeleting] = useState<string | null>(null);
	const [deleteError, setDeleteError] = useState<string | null>(null);
	const deletionOperations = useRef(new Map<string, string>());
  const { value, error, loading, reload } = useFinanceView(readWorkspaceTasks);
  const tasks = value?.tasks ?? [];
  const list = f === 'all' ? tasks : tasks.filter((t) => t.status === f);
	const deletable = (status: string) => ['draft','pending_escrow','escrowed','matching','overview_generating','awaiting_selection'].includes(status);
	const remove = async (task: (typeof tasks)[number]) => {
		if (deleting) return;
		setDeleting(task.id); setDeleteError(null);
		try {
			let operation = deletionOperations.current.get(task.id);
			if (!operation) { operation = crypto.randomUUID(); deletionOperations.current.set(task.id, operation); }
			const result = await requestTaskDeletion(task.id, task.aggregateVersion, operation);
			if (result.refundRequired) {
				const ethereum = (window as typeof window & { ethereum?: WalletProvider }).ethereum;
				if (!ethereum) throw new Error('未检测到以太坊兼容钱包，无法退回托管资金。');
				await submitTaskRefundTransaction(ethereum, result);
				toast.success('退款交易已提交，链上确认后任务将自动删除');
			} else {
				toast.success('任务已删除');
			}
			setConfirming(null); await reload();
		} catch (cause) { setDeleteError(cause instanceof Error ? cause.message : '删除任务失败，请重试。'); }
		finally { setDeleting(null); }
	};

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
	  {deleteError ? <Panel className="p-4 text-rose-200"><span role="alert">{deleteError}</span></Panel> : null}

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
				<div className="flex items-center gap-2">
                {act && <GhostButton onClick={() => nav(t.status === 'pending_escrow' ? `/publisher/tasks/${encodeURIComponent(t.id)}/funding` : act.to === '/publisher/recommendations' ? `${act.to}?taskId=${encodeURIComponent(t.id)}` : ['formal_review','formal_generating'].includes(t.status) ? `/publisher/tasks/${encodeURIComponent(t.id)}/delivery` : act.to)}>{act.label}</GhostButton>}
				{t.deletionPending ? <Pill tone="amber">退款确认中</Pill> : deletable(t.status) && (confirming === t.id ? <><span className="text-[11px] text-amber-200">{['escrowed','matching','overview_generating','awaiting_selection'].includes(t.status) ? '将先退回托管资金，确认删除？' : '确认删除此任务？'}</span><button type="button" disabled={deleting === t.id} onClick={() => void remove(t)} className="rounded-lg border border-rose-300/40 px-3 py-2 text-[12px] text-rose-200 disabled:opacity-40">{deleting === t.id ? '处理中…' : '确认删除'}</button><button type="button" onClick={() => setConfirming(null)} className="px-2 py-2 text-[12px] text-[var(--ap-muted)]">取消</button></> : <button type="button" aria-label={`删除任务 ${t.title}`} onClick={() => setConfirming(t.id)} className="grid h-9 w-9 place-items-center rounded-lg border border-rose-300/25 text-rose-300 hover:bg-rose-300/10"><Trash2 size={15} /></button>)}
				</div>
              </div>
            </Panel>
          );
        })}
      </div>
    </Page>
  );
}
