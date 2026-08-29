import { cookies } from "next/headers";
import { NextResponse } from "next/server";
import { aggregateEngineWorkspace, mapEngineFailure, type EngineWorkspaceKind } from "./engine";
import { sessionCookieName } from "./session";

export async function workspaceRoute(kind: EngineWorkspaceKind) {
  const token = (await cookies()).get(sessionCookieName)?.value;
  if (!token) return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  try {
    const result = await aggregateEngineWorkspace(kind, token);
    return NextResponse.json(result.body, { status: result.status });
  } catch (error) {
    const failure = mapEngineFailure(error);
    return NextResponse.json(failure.body, { status: failure.status });
  }
}
