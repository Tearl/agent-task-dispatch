import type { Metadata } from "next";
import type { ReactNode } from "react";

export const metadata: Metadata = {
  title: "AI Agent Platform",
  description: "发布需求、智能匹配、链上托管、安全交付。",
};

export default function RootLayout({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <html lang="zh-CN">
      <body style={{ margin: 0, fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif" }}>
        {children}
      </body>
    </html>
  );
}
