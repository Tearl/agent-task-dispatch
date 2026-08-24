import { cookies } from "next/headers";
import { NextResponse } from "next/server";
import { aggregateEngineWorkspace, type EngineWorkspaceKind, InvalidEngineResponseError } from "./engine";
import { sessionCookieName } from "./session";

export async function workspaceRoute(kind: EngineWorkspaceKind) {
  const token = (await cookies()).get(sessionCookieName)?.value;
  if (!token) return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  try {
    const result = await aggregateEngineWorkspace(kind, token);
    return NextResponse.json(result.body, { status: result.status });
  } catch (error) {
    if (error instanceof InvalidEngineResponseError) return NextResponse.json({ error: "invalid engine response" }, { status: 502 });
    return NextResponse.json({ error: "engine service temporarily unavailable" }, { status: 503 });
  }
}
