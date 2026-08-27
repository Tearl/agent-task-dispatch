import { Activity, Boxes, Cpu, PlusCircle, RefreshCw, Save, ShieldCheck, X } from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router";
import { toast } from "sonner";

import { Page } from "../../components/AppShell";
import { Bar, CtaButton, GhostButton, PageHeader, Panel, Pill, SectionTitle } from "../../components/kit/primitives";
import { checkAgentHealth, readAgentProfile, readWorkspaceAgents, updateAgentProfile } from "../../lib/platform-api";
import { useFinanceView } from "../../lib/use-finance-view";

export default function AgentIntegration() {
  const navigate = useNavigate();
  const {value,error,loading,reload}=useFinanceView(readWorkspaceAgents);
  const agents=value?.agents??[];
  const [editingAgentID,setEditingAgentID]=useState<string|null>(null);
  const [endpointUrl,setEndpointUrl]=useState("");
  const [saving,setSaving]=useState(false);

  const checkHealth=async(agent:typeof agents[number])=>{try{await checkAgentHealth(agent.id,agent.aggregateVersion);toast.success(`${agent.name} 健康检查已完成`);reload();}catch(cause){toast.error(cause instanceof Error?cause.message:"健康检查失败");}};
  const editEndpoint=(agent:typeof agents[number])=>{setEditingAgentID(agent.id);setEndpointUrl(agent.endpointUrl??"");};
  const saveEndpoint=async(agent:typeof agents[number])=>{if(saving)return;setSaving(true);try{const {agent:profile}=await readAgentProfile(agent.id);const updated=await updateAgentProfile(profile,endpointUrl.trim(),`${agent.id}:profile:${crypto.randomUUID()}`);await checkAgentHealth(agent.id,updated.aggregateVersion);toast.success(`${agent.name} 端点已更新并通过健康检查`);setEditingAgentID(null);reload();}catch(cause){toast.error(cause instanceof Error?cause.message:"更新 Agent 端点失败");}finally{setSaving(false);}};

  return (
    <Page>
      <PageHeader
        title="Agent 管理与接入"
        subtitle="调用配置、凭证脱敏、健康检查与协议校验"
        actions={
          <CtaButton icon={PlusCircle} onClick={() => navigate("/agent/integration/new")}>
            接入新 Agent
          </CtaButton>
        }
      />

      <div className="grid gap-4 lg:grid-cols-3">
        {agents.map((agent) => (
          <Panel key={agent.id} hover className="p-5">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-3">
                <span className="grid h-10 w-10 place-items-center rounded-xl bg-[rgba(139,92,246,0.15)] text-[var(--ap-violet)]">
                  <Cpu size={18} />
                </span>
                <div>
                  <div className="text-[15px] text-[var(--ap-text)]">{agent.name}</div>
                  <div className="text-[12px] text-[var(--ap-muted)]">{agent.category}</div>
                </div>
              </div>
              <Pill tone={agent.health === "healthy" ? "green" : agent.health === "degraded" ? "amber" : "red"} dot>
                {agent.health === "healthy" ? "健康" : agent.health === "degraded" ? "降级" : agent.health}
              </Pill>
            </div>

            <div className="mt-4 space-y-3 text-[13px]">
              <div>
                <div className="flex items-center justify-between text-[var(--ap-muted)]">
                  <span className="inline-flex items-center gap-1.5">
                    <Activity size={13} /> 健康度
                  </span>
                  <span>{healthPercent(agent.health)}%</span>
                </div>
                <div className="mt-1.5">
                  <Bar value={healthPercent(agent.health)} tone={agent.health === "healthy" ? "#34d399" : agent.health === "degraded" ? "#fbbf24" : "#fb7185"} />
                </div>
              </div>
              <div className="flex items-center justify-between gap-3">
                <span className="text-[var(--ap-muted)]">调用端点</span>
                <span className="max-w-[150px] truncate text-[var(--ap-text-2)]">{agent.endpointUrl || "未配置"}</span>
              </div>
              <div className="flex items-center justify-between gap-3">
                <span className="inline-flex items-center gap-1.5 text-[var(--ap-muted)]">
                  <ShieldCheck size={13} /> 协议校验
                </span>
                <Pill tone={agent.status === "active" ? "green" : "amber"}>agent-execution-v1</Pill>
              </div>
            </div>

            <div className="mt-4 flex flex-wrap gap-2">
              <GhostButton
                icon={RefreshCw}
                className="flex-1"
                onClick={() => void checkHealth(agent)}
              >
                健康检查
              </GhostButton>
              <GhostButton className="flex-1" onClick={()=>editEndpoint(agent)}>调用配置</GhostButton>
            </div>
            {editingAgentID===agent.id?<form className="mt-4 space-y-3 border-t border-[var(--ap-border)] pt-4" onSubmit={(event)=>{event.preventDefault();void saveEndpoint(agent);}}><label htmlFor={`agent-endpoint-${agent.id}`} className="text-[11px] text-[var(--ap-muted)]">协议基础地址</label><input id={`agent-endpoint-${agent.id}`} type="url" required value={endpointUrl} onChange={(event)=>setEndpointUrl(event.target.value)} className="form-input" placeholder="https://agent.example.com" /><div className="flex gap-2"><button type="submit" disabled={saving} className="ap-cta inline-flex flex-1 items-center justify-center gap-2 rounded-xl px-3 py-2 text-[12px] disabled:opacity-50"><Save size={14}/>{saving?"保存中…":"保存并检查"}</button><GhostButton icon={X} disabled={saving} onClick={()=>setEditingAgentID(null)}>取消</GhostButton></div></form>:null}
          </Panel>
        ))}
      </div>

      {loading?<div role="status" className="py-8 text-center text-[var(--ap-muted)]">正在读取 Agent 配置…</div>:null}
      {error?<Panel className="p-4 text-rose-200"><span role="alert">{error}</span><div className="mt-2"><GhostButton onClick={reload}>重试</GhostButton></div></Panel>:null}

      <Panel className="p-6">
        <SectionTitle
          right={
            <Pill tone="cyan" dot>
              安全存储
            </Pill>
          }
        >
          凭证与密钥（脱敏展示）
        </SectionTitle>
        <div className="space-y-3">
          {agents.map((agent) => (
            <div
              key={agent.id}
              className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-[var(--ap-border)] bg-[rgba(10,18,38,0.4)] px-4 py-3"
            >
              <div>
                <div className="text-[14px] text-[var(--ap-text)]">{agent.name} · 运行凭据</div>
                <div className="mt-0.5 font-mono text-[13px] text-[var(--ap-muted)]">
                  {agent.currentCredentialVersion ? `已配置 · 版本 ${agent.currentCredentialVersion}` : "尚未配置"}
                </div>
              </div>
              <Pill tone="gray">明文不可读取</Pill>
            </div>
          ))}
        </div>
        <p className="mt-3 flex items-center gap-1.5 text-[12px] text-[var(--ap-muted)]">
          <Boxes size={13} /> 凭证以加密形式存储，平台与管理员均无法查看明文。
        </p>
      </Panel>
    </Page>
  );
}

function healthPercent(health:string){return health==="healthy"?100:health==="degraded"?60:health==="unhealthy"?10:0;}
