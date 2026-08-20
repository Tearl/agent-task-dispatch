import { useNavigate } from 'react-router';
import { toast } from 'sonner';
import {
  Wallet, MonitorSmartphone, ShieldCheck, Repeat, LogOut, Info, Check, KeyRound,
} from 'lucide-react';
import { Page } from '../../components/AppShell';
import { PageHeader, Panel, SectionTitle, Pill, GhostButton, InfoNote } from '../../components/kit/primitives';
import { useSession, shortAddr } from '../../lib/session';
import { ROLES, CLIENT_ROLES } from '../../lib/roles';

const SESSIONS = [
  { device: 'Chrome · macOS', loc: '上海', current: true, time: '当前会话' },
  { device: 'Safari · iPhone', loc: '上海', current: false, time: '2 小时前' },
  { device: 'Chrome · Windows', loc: '北京', current: false, time: '昨天' },
];

const SECURITY = [
  { t: '钱包签名登录', ip: '103.44.xx.xx', time: '今天 08:12', ok: true },
  { t: '新设备验证通过', ip: '103.44.xx.xx', time: '昨天 19:30', ok: true },
  { t: '异常登录已拦截', ip: '45.12.xx.xx', time: '3 天前', ok: false },
];

export default function Account() {
  const { address, role, switchRole, disconnect } = useSession();
  const nav = useNavigate();
  return (
    <Page>
      <PageHeader title="账户与安全" subtitle="钱包、会话、安全记录与 C 端角色切换" />

      <InfoNote tone="cyan">
        <span className="inline-flex items-center gap-1.5"><Info size={14} />此处仅包含 C 端角色（发布方 / 开发者 / 仲裁者），不含管理员权限。</span>
      </InfoNote>

      <div className="grid gap-6 lg:grid-cols-[1fr_1.3fr]">
        <div className="space-y-6">
          <Panel strong className="p-6">
            <SectionTitle right={<Pill tone="green" dot>已连接</Pill>}>钱包</SectionTitle>
            <div className="flex items-center gap-3">
              <span className="grid h-11 w-11 place-items-center rounded-xl bg-[rgba(34,211,238,0.15)] text-[var(--ap-cyan)]"><Wallet size={20} /></span>
              <div>
                <div className="font-mono text-[15px] text-[var(--ap-text)]">{shortAddr(address) || '0x…'}</div>
                <div className="text-[12px] text-[var(--ap-muted)]">MetaMask · 以太坊测试网</div>
              </div>
            </div>
            <div className="mt-4 flex gap-2">
              <GhostButton icon={KeyRound} onClick={() => toast.success('已发起签名验证')}>重新签名</GhostButton>
              <GhostButton icon={LogOut} onClick={() => { disconnect(); nav('/'); }}>断开连接</GhostButton>
            </div>
          </Panel>

          <Panel className="p-6">
            <SectionTitle>角色切换</SectionTitle>
            <div className="space-y-2">
              {CLIENT_ROLES.map((r) => {
                const cfg = ROLES[r];
                const active = role === r;
                return (
                  <button key={r} onClick={() => { switchRole(r); nav(cfg.home); }}
                    className="flex w-full items-center justify-between rounded-xl border px-4 py-3 text-left transition-colors"
                    style={{ borderColor: active ? 'var(--ap-border-strong)' : 'var(--ap-border)', background: active ? 'var(--ap-cyan-soft)' : 'transparent' }}>
                    <span className="flex items-center gap-2 text-[14px]" style={{ color: active ? '#a5f3fc' : 'var(--ap-text-2)' }}>
                      <span className="h-2 w-2 rounded-full" style={{ background: cfg.accent }} />{cfg.name}
                    </span>
                    {active ? <Check size={16} className="text-[var(--ap-cyan)]" /> : <Repeat size={15} className="text-[var(--ap-muted)]" />}
                  </button>
                );
              })}
            </div>
          </Panel>
        </div>

        <div className="space-y-6">
          <Panel className="p-6">
            <SectionTitle right={<GhostButton onClick={() => toast.success('已登出其他所有设备')}>登出其他设备</GhostButton>}>活跃会话</SectionTitle>
            <div className="space-y-3">
              {SESSIONS.map((s) => (
                <div key={s.device} className="flex items-center justify-between rounded-xl border border-[var(--ap-border)] px-4 py-3">
                  <div className="flex items-center gap-3">
                    <MonitorSmartphone size={17} className="text-[var(--ap-text-2)]" />
                    <div>
                      <div className="text-[14px] text-[var(--ap-text)]">{s.device}</div>
                      <div className="text-[12px] text-[var(--ap-muted)]">{s.loc} · {s.time}</div>
                    </div>
                  </div>
                  {s.current && <Pill tone="green">当前</Pill>}
                </div>
              ))}
            </div>
          </Panel>

          <Panel className="p-6">
            <SectionTitle right={<ShieldCheck size={16} className="text-[var(--ap-success)]" />}>安全记录</SectionTitle>
            <div className="space-y-3">
              {SECURITY.map((s) => (
                <div key={s.t} className="flex items-center justify-between rounded-xl border border-[var(--ap-border)] px-4 py-3">
                  <div className="flex items-center gap-3">
                    <Pill tone={s.ok ? 'green' : 'red'} dot>{s.ok ? '正常' : '拦截'}</Pill>
                    <span className="text-[13px] text-[var(--ap-text-2)]">{s.t}</span>
                  </div>
                  <span className="text-[12px] text-[var(--ap-muted)]">{s.ip} · {s.time}</span>
                </div>
              ))}
            </div>
          </Panel>
        </div>
      </div>
    </Page>
  );
}
