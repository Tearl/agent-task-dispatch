import {
  ArrowLeft,
  CheckCircle2,
  KeyRound,
  MonitorSmartphone,
  ServerCog,
  ShieldAlert,
  Wallet,
} from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router";

import { CtaButton } from "../components/kit/primitives";
import { useSession } from "../lib/session";

export default function AdminLogin() {
  const navigate = useNavigate();
  const { adminLogin } = useSession();
  const [step, setStep] = useState<0 | 1 | 2>(0);
  const [otp, setOtp] = useState("");

  const checks = [
    { icon: Wallet, label: "钱包签名认证", done: step >= 1 },
    { icon: MonitorSmartphone, label: "设备风险检查（可信设备）", done: step >= 1 },
    { icon: KeyRound, label: "二次验证 (2FA)", done: step >= 2 },
  ];

  const finish = () => {
    adminLogin();
    navigate("/admin");
  };

  return (
    <div className="ap-app-bg relative grid min-h-svh w-full place-items-center overflow-hidden px-5 py-20">
      <div className="ap-grid-texture absolute inset-0 opacity-30" />
      <button
        type="button"
        onClick={() => navigate("/")}
        className="absolute left-5 top-5 inline-flex items-center gap-1.5 text-[13px] text-[var(--ap-muted)] hover:text-[var(--ap-text-2)] sm:left-6 sm:top-6"
      >
        <ArrowLeft size={15} /> 返回主站
      </button>

      <div className="ap-glass-strong relative w-full max-w-[440px] rounded-3xl p-6 sm:p-9">
        <div className="flex items-center gap-2 text-[12px] tracking-widest text-[var(--ap-info)]">
          <ShieldAlert size={14} /> 独立管理后台 · 受限访问
        </div>
        <div className="mt-5 flex items-center gap-3">
          <span className="grid h-12 w-12 place-items-center rounded-xl bg-[rgba(56,189,248,0.15)] text-[var(--ap-info)]">
            <ServerCog size={22} />
          </span>
          <div>
            <h1 className="text-[20px] text-white">管理员安全登录</h1>
            <p className="text-[13px] text-[var(--ap-muted)]">钱包签名 · 设备风险 · 二次验证</p>
          </div>
        </div>

        <div className="mt-7 space-y-3">
          {checks.map(({ icon: Icon, label, done }) => (
            <div
              key={label}
              className="flex items-center justify-between rounded-xl border border-[var(--ap-border)] bg-[rgba(10,18,38,0.5)] px-4 py-3"
            >
              <span className="flex items-center gap-3 text-[14px] text-[var(--ap-text-2)]">
                <Icon size={17} className="text-[var(--ap-info)]" />
                {label}
              </span>
              {done ? (
                <CheckCircle2 size={18} className="text-[var(--ap-success)]" />
              ) : (
                <span className="text-[12px] text-[var(--ap-muted)]">待完成</span>
              )}
            </div>
          ))}
        </div>

        {step === 2 ? (
          <div className="mt-5">
            <label htmlFor="admin-otp" className="text-[13px] text-[var(--ap-muted)]">
              输入 6 位动态验证码
            </label>
            <input
              id="admin-otp"
              inputMode="numeric"
              value={otp}
              onChange={(event) => setOtp(event.target.value.replace(/\D/g, "").slice(0, 6))}
              placeholder="• • • • • •"
              className="mt-2 w-full rounded-xl border border-[var(--ap-border)] bg-[rgba(5,9,20,0.6)] px-4 py-3 text-center text-[18px] tracking-[0.5em] text-white outline-none focus:border-[var(--ap-border-strong)]"
            />
          </div>
        ) : null}

        <div className="mt-7">
          {step === 0 ? (
            <CtaButton full icon={Wallet} onClick={() => setStep(1)}>
              钱包签名验证
            </CtaButton>
          ) : null}
          {step === 1 ? (
            <CtaButton full icon={KeyRound} onClick={() => setStep(2)}>
              发起二次验证
            </CtaButton>
          ) : null}
          {step === 2 ? (
            <CtaButton full icon={CheckCircle2} onClick={finish}>
              进入管理后台
            </CtaButton>
          ) : null}
        </div>

        <p className="mt-5 text-center text-[12px] text-[var(--ap-muted)]">
          管理后台仅供已授权运营人员使用，所有操作将写入只追加审计日志。
        </p>
      </div>
    </div>
  );
}
