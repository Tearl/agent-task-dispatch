import { Info, Search, ShieldCheck } from "lucide-react";
import { toast } from "sonner";

import { Page } from "../../components/AppShell";
import { GhostButton, InfoNote, PageHeader, Panel, Pill } from "../../components/kit/primitives";

const USERS = [
  { addr: "0x8f2a…c21d", roles: ["发布方"], scope: "本人数据", status: "active", last: "10 分钟前" },
  { addr: "0x21be…9a4f", roles: ["开发者", "发布方"], scope: "本人数据", status: "active", last: "1 小时前" },
  { addr: "0x7cd0…1e88", roles: ["仲裁者"], scope: "脱敏案件", status: "restricted", last: "3 小时前" },
  { addr: "0x6d3f…4e02", roles: ["发布方"], scope: "本人数据", status: "suspended", last: "昨天" },
];

const TONE = { active: "green", restricted: "amber", suspended: "red" } as const;
const LABEL = { active: "正常", restricted: "受限", suspended: "停用" };

export default function AdminUsers() {
  return (
    <Page>
      <PageHeader title="用户与角色" subtitle="角色授权、账户限制、数据范围与变更审计" />

      <InfoNote tone="blue">
        <span className="inline-flex items-center gap-1.5">
          <Info size={14} />
          管理员可授权角色与限制账户，但不能查看用户私钥/凭证明文，所有变更写入只追加审计日志。
        </span>
      </InfoNote>

      <Panel className="flex items-center gap-3 p-4">
        <div className="flex flex-1 items-center gap-2 rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.5)] px-3 py-2">
          <Search size={16} className="text-[var(--ap-muted)]" />
          <input
            aria-label="搜索用户"
            placeholder="按钱包地址或角色搜索…"
            className="w-full bg-transparent text-[14px] text-white outline-none placeholder:text-[var(--ap-muted)]"
          />
        </div>
      </Panel>

      <Panel className="p-5">
        <div className="ap-scroll overflow-x-auto">
          <table className="w-full min-w-[820px] text-[13px]">
            <thead>
              <tr className="text-left text-[var(--ap-muted)]">
                <th className="pb-3 font-normal">钱包地址</th>
                <th className="pb-3 font-normal">已授权角色</th>
                <th className="pb-3 font-normal">数据范围</th>
                <th className="pb-3 font-normal">状态</th>
                <th className="pb-3 font-normal">最近活跃</th>
                <th className="pb-3 text-right font-normal">操作</th>
              </tr>
            </thead>
            <tbody>
              {USERS.map((user) => (
                <tr key={user.addr} className="border-t border-[var(--ap-border)]">
                  <td className="py-3 font-mono text-[var(--ap-text)]">{user.addr}</td>
                  <td className="py-3">
                    <div className="flex gap-1.5">
                      {user.roles.map((role) => (
                        <Pill key={role} tone="cyan">
                          {role}
                        </Pill>
                      ))}
                    </div>
                  </td>
                  <td className="py-3 text-[var(--ap-text-2)]">{user.scope}</td>
                  <td className="py-3">
                    <Pill tone={TONE[user.status as keyof typeof TONE]}>{LABEL[user.status as keyof typeof LABEL]}</Pill>
                  </td>
                  <td className="py-3 text-[var(--ap-muted)]">{user.last}</td>
                  <td className="py-3 text-right">
                    <div className="flex justify-end gap-2">
                      <GhostButton onClick={() => toast.success("已更新角色授权")}>授权</GhostButton>
                      <GhostButton onClick={() => toast.success("已更新账户限制")}>限制</GhostButton>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <p className="mt-4 flex items-center gap-1.5 text-[12px] text-[var(--ap-muted)]">
          <ShieldCheck size={13} /> 角色授权遵循最小权限原则，管理员不出现在 C 端登录与普通用户菜单。
        </p>
      </Panel>
    </Page>
  );
}
