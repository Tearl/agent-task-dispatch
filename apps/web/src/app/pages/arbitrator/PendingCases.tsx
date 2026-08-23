import { RefreshCw, Scale } from "lucide-react";
import { useEffect, useState } from "react";
import { useNavigate } from "react-router";
import { Page } from "../../components/AppShell";
import { CtaButton, GhostButton, InfoNote, PageHeader, Panel, Pill, SectionTitle } from "../../components/kit/primitives";
import { readDisputes, type DisputeView } from "../../lib/platform-api";

export default function PendingCases(){const nav=useNavigate();const[cases,setCases]=useState<DisputeView[]>([]),[error,setError]=useState("");const load=async()=>{setError("");try{setCases((await readDisputes()).cases)}catch(cause){setError(cause instanceof Error?cause.message:"案件读取失败。")}};useEffect(()=>{void load()},[]);return <Page><PageHeader title="待处理案件" subtitle="仅显示 Engine 按角色授权的案件" actions={<GhostButton icon={RefreshCw} onClick={()=>void load()}>刷新</GhostButton>}/>{error?<InfoNote tone="red"><span role="alert">{error}</span></InfoNote>:null}<div className="space-y-3">{cases.map((view)=><Panel key={view.case.id} className="p-5"><SectionTitle right={<Pill tone="amber">{view.case.state}</Pill>}>{view.case.id}</SectionTitle><div className="grid gap-2 text-[12px] sm:grid-cols-3"><span>证据 {view.case.evidence.length}/12</span><span>举证截止 {date(view.case.evidenceDeadline)}</span><span>决定截止 {date(view.case.decisionDeadline)}</span></div><CtaButton className="mt-4" icon={Scale} onClick={()=>nav("/arbitrator/review")}>进入权威审理</CtaButton></Panel>)}</div></Page>}
function date(value?:string){return value?new Date(value).toLocaleString("zh-CN"):"尚未开始"}
