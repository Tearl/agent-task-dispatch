import { useState } from 'react';
import { toast } from 'sonner';
import {
  Bell, ListChecks, Wallet, MessageSquareWarning, ShieldCheck, Check, Settings2,
} from 'lucide-react';
import { Page } from '../../components/AppShell';
import { PageHeader, Panel, SectionTitle, Pill, GhostButton } from '../../components/kit/primitives';
import { NOTIFICATIONS, type Notification } from '../../lib/mock';

const META: Record<Notification['type'], { icon: typeof Bell; tone: 'cyan' | 'green' | 'red' | 'amber'; label: string }> = {
  task: { icon: ListChecks, tone: 'cyan', label: '任务' },
  fund: { icon: Wallet, tone: 'green', label: '资金' },
  dispute: { icon: MessageSquareWarning, tone: 'red', label: '争议' },
  security: { icon: ShieldCheck, tone: 'amber', label: '安全' },
};

const FILTERS: (Notification['type'] | 'all')[] = ['all', 'task', 'fund', 'dispute', 'security'];

export default function Notifications() {
  const [items, setItems] = useState(NOTIFICATIONS);
  const [f, setF] = useState<Notification['type'] | 'all'>('all');
  const [prefs, setPrefs] = useState({ task: true, fund: true, dispute: true, security: true });

  const list = f === 'all' ? items : items.filter((n) => n.type === f);
  const unread = items.filter((n) => !n.read).length;

  return (
    <Page>
      <PageHeader
        title="消息中心"
        subtitle="任务、资金、争议与安全通知及通知偏好"
        actions={<GhostButton icon={Check} onClick={() => { setItems((x) => x.map((n) => ({ ...n, read: true }))); toast.success('已全部标记为已读'); }}>全部已读</GhostButton>}
      />

      <div className="grid gap-6 lg:grid-cols-[1.5fr_1fr]">
        <div className="space-y-4">
          <div className="flex flex-wrap items-center gap-2">
            {FILTERS.map((x) => (
              <button key={x} onClick={() => setF(x)}
                className="rounded-full border px-3.5 py-1.5 text-[13px] transition-colors"
                style={{
                  borderColor: f === x ? 'var(--ap-border-strong)' : 'var(--ap-border)',
                  background: f === x ? 'var(--ap-cyan-soft)' : 'transparent',
                  color: f === x ? '#a5f3fc' : 'var(--ap-text-2)',
                }}>{x === 'all' ? `全部 (${unread})` : META[x].label}</button>
            ))}
          </div>

          <div className="space-y-3">
            {list.map((n) => {
              const m = META[n.type];
              return (
                <Panel key={n.id} hover className="p-4 flex items-center gap-3">
                  <span className="grid h-9 w-9 shrink-0 place-items-center rounded-lg"
                    style={{ background: 'rgba(34,211,238,0.1)' }}>
                    <m.icon size={16} className="text-[var(--ap-cyan)]" />
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <Pill tone={m.tone}>{m.label}</Pill>
                      {!n.read && <span className="h-1.5 w-1.5 rounded-full bg-[var(--ap-cyan)]" />}
                    </div>
                    <div className="mt-1 text-[14px] text-[var(--ap-text)]">{n.title}</div>
                    <div className="text-[12px] text-[var(--ap-muted)]">{n.time}</div>
                  </div>
                  {!n.read && (
                    <button onClick={() => setItems((x) => x.map((it) => it.id === n.id ? { ...it, read: true } : it))}
                      className="text-[12px] text-[var(--ap-cyan)]">标记已读</button>
                  )}
                </Panel>
              );
            })}
          </div>
        </div>

        <Panel className="p-6 h-fit">
          <SectionTitle right={<Settings2 size={16} className="text-[var(--ap-cyan)]" />}>通知偏好</SectionTitle>
          <div className="space-y-3">
            {(Object.keys(prefs) as (keyof typeof prefs)[]).map((k) => (
              <div key={k} className="flex items-center justify-between rounded-xl border border-[var(--ap-border)] px-4 py-3">
                <span className="flex items-center gap-2 text-[14px] text-[var(--ap-text-2)]">
                  {(() => { const I = META[k].icon; return <I size={16} className="text-[var(--ap-cyan)]" />; })()}
                  {META[k].label}通知
                </span>
                <button onClick={() => setPrefs((p) => ({ ...p, [k]: !p[k] }))}
                  className="relative h-6 w-11 rounded-full transition-colors"
                  style={{ background: prefs[k] ? 'var(--ap-aqua)' : 'rgba(255,255,255,0.12)' }}>
                  <span className="absolute top-0.5 h-5 w-5 rounded-full bg-white transition-all" style={{ left: prefs[k] ? 22 : 2 }} />
                </button>
              </div>
            ))}
          </div>
          <p className="mt-4 text-[12px] text-[var(--ap-muted)]">关闭后仍会在站内保留记录，仅不再推送提醒。</p>
        </Panel>
      </div>
    </Page>
  );
}

