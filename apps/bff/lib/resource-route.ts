import { cookies } from "next/headers";
import { NextResponse } from "next/server";
import { aggregateEngineResource, InvalidResourceIdError, mapEngineFailure, type EngineResourceKind } from "./engine";
import { sessionCookieName } from "./session";

export async function resourceRoute(kind: EngineResourceKind, id: string): Promise<NextResponse> {
  const token = (await cookies()).get(sessionCookieName)?.value;
  if (!token) return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  try {
    const result = await aggregateEngineResource(kind, id, token);
    return NextResponse.json(result.body, { status: result.status });
  } catch (error) {
    if (error instanceof InvalidResourceIdError) return NextResponse.json({ error: "invalid resource id" }, { status: 400 });
    const failure = mapEngineFailure(error);
    return NextResponse.json(failure.body, { status: failure.status });
  }
}
