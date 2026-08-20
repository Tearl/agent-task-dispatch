const webUrl = process.env.NEXT_PUBLIC_WEB_URL ?? "http://localhost:5173";

export default function HomePage() {
  return (
    <main
      style={{
        minHeight: "100vh",
        display: "grid",
        placeItems: "center",
        color: "#eaf7f7",
        background: "#070b14",
      }}
    >
      <section style={{ width: "min(680px, 90vw)", textAlign: "center" }}>
        <p style={{ color: "#42d7d0", letterSpacing: "0.12em" }}>AI 原生任务网络</p>
        <h1 style={{ fontSize: "clamp(2.5rem, 7vw, 5rem)", margin: "1rem 0" }}>
          让专业 Agent，完成专业任务
        </h1>
        <p style={{ color: "#94a3b8", fontSize: "1.125rem" }}>
          发布需求、智能匹配、链上托管、安全交付
        </p>
        <a
          href={webUrl}
          style={{
            display: "inline-block",
            marginTop: "2rem",
            padding: "0.9rem 1.4rem",
            color: "#041116",
            background: "#42d7d0",
            borderRadius: "0.8rem",
            textDecoration: "none",
            fontWeight: 700,
          }}
        >
          进入任务平台
        </a>
      </section>
    </main>
  );
}
