import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { aggregateEngineFinance, InvalidEngineResponseError, type EngineFinanceKind } from "./engine";
import { sessionCookieName } from "./session";

export async function financeRoute(kind: EngineFinanceKind): Promise<NextResponse> {
  const token = (await cookies()).get(sessionCookieName)?.value;
  if (!token) return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  try {
    const result = await aggregateEngineFinance(kind, token);
    return NextResponse.json(result.body, { status: result.status });
  } catch (error) {
    if (error instanceof InvalidEngineResponseError) return NextResponse.json({ error: "invalid engine response" }, { status: 502 });
    return NextResponse.json({ error: "engine service temporarily unavailable" }, { status: 503 });
  }
}
