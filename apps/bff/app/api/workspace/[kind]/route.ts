import { NextResponse } from "next/server";
import { workspaceRoute } from "../../../../lib/workspace-route";

const kinds = new Set(["tasks", "agents", "marketplace", "notifications"] as const);

export async function GET(_request: Request, { params }: { params: Promise<{ kind: string }> }) {
  const { kind } = await params;
  if (!kinds.has(kind as "tasks")) return NextResponse.json({ error: "not found" }, { status: 404 });
  return workspaceRoute(kind as "tasks" | "agents" | "marketplace" | "notifications");
}
