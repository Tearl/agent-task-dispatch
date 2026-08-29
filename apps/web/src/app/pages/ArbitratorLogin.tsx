import { ArrowLeft, Scale, ShieldCheck, Wallet } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router";

import { CtaButton } from "../components/kit/primitives";
import { authenticateWallet, clientRolesForEngineRoles, type WalletProvider } from "../lib/platform-api";
import { useSession } from "../lib/session";

export default function ArbitratorLogin() {
  const navigate = useNavigate();
  const { connected, authorizedRoles, connect, disconnect } = useSession();
  const [error, setError] = useState<string | null>(null);
  const [connecting, setConnecting] = useState(false);
  const connectInFlight = useRef(false);

  useEffect(() => {
    if (connected && authorizedRoles.includes("arbitrator")) navigate("/arbitrator", { replace: true });
  }, [authorizedRoles, connected, navigate]);

  const login = async () => {
    if (connectInFlight.current) return;
    const ethereum = (window as typeof window & { ethereum?: WalletProvider }).ethereum;
    if (!ethereum) {
      setError("未检测到钱包，请先安装 MetaMask 或其他以太坊兼容钱包。");
      return;
    }
    connectInFlight.current = true;
    setConnecting(true);
    setError(null);
    try {
      const session = await authenticateWallet(ethereum);
      if (!clientRolesForEngineRoles(session.roles).includes("arbitrator")) {
        await disconnect();
        setError("该钱包没有仲裁员权限。");
        return;
      }
      connect(session, "arbitrator");
      navigate("/arbitrator", { replace: true });
    } catch (cause) {
      setError(cause instanceof Error && cause.message ? `登录失败：${cause.message}` : "钱包认证未完成，请重试。");
    } finally {
      connectInFlight.current = false;
      setConnecting(false);
    }
  };

  return (
    <div className="ap-app-bg relative grid min-h-svh w-full place-items-center overflow-hidden px-5 py-20">
      <div className="ap-grid-texture absolute inset-0 opacity-30" />
      <button type="button" onClick={() => navigate("/")} className="absolute left-5 top-5 inline-flex items-center gap-1.5 text-[13px] text-[var(--ap-muted)] hover:text-[var(--ap-text-2)] sm:left-6 sm:top-6">
        <ArrowLeft size={15} /> 返回主站
      </button>
      <div className="ap-glass-strong relative w-full max-w-[440px] rounded-3xl p-6 sm:p-9">
        <div className="flex items-center gap-2 text-[12px] tracking-widest text-[var(--ap-info)]"><ShieldCheck size={14} /> 独立仲裁工作台 · 受限访问</div>
        <div className="mt-5 flex items-center gap-3">
          <span className="grid h-12 w-12 place-items-center rounded-xl bg-[rgba(56,189,248,0.15)] text-[var(--ap-info)]"><Scale size={22} /></span>
          <div><h1 className="text-[20px] text-white">仲裁员安全登录</h1><p className="text-[13px] text-[var(--ap-muted)]">钱包签名 · 服务端角色校验</p></div>
        </div>
        <div className="mt-7 rounded-xl border border-[var(--ap-border)] bg-[rgba(10,18,38,0.5)] p-4 text-[13px] leading-6 text-[var(--ap-text-2)]">
          登录守卫只负责页面访问；Engine 仍会对每次案件读取、初裁和复核执行最终角色、资源和领域状态授权。
        </div>
        {error ? <div role="alert" className="mt-4 rounded-xl border border-[rgba(248,113,113,0.35)] bg-[rgba(248,113,113,0.08)] p-3 text-[13px] text-[var(--ap-danger)]">{error}</div> : null}
        <div className="mt-7"><CtaButton full icon={Wallet} disabled={connecting} onClick={() => void login()}>{connecting ? "正在验证仲裁员权限…" : "连接钱包并登录"}</CtaButton></div>
        <p className="mt-5 text-center text-[12px] text-[var(--ap-muted)]">仅 Engine 授权的仲裁员钱包可访问案件。</p>
      </div>
    </div>
  );
}
