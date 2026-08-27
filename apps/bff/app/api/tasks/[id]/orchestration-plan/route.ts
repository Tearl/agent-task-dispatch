import { cookies } from "next/headers";
import { NextResponse } from "next/server";
import { forwardEngineRead } from "../../../../../lib/engine";
import { sessionCookieName } from "../../../../../lib/session";

export async function GET(_: Request, context: { params: Promise<{ id: string }> }) {
  const token = (await cookies()).get(sessionCookieName)?.value;
  if (!token) return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  const { id } = await context.params;
  const result = await forwardEngineRead(`/v1/tasks/${encodeURIComponent(id)}/orchestration-plan`, token);
  return NextResponse.json(result.body, { status: result.status });
}
