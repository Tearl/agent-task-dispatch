import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { aggregateEngineFinance, mapEngineFailure, type EngineFinanceKind } from "./engine";
import { sessionCookieName } from "./session";

export async function financeRoute(kind: EngineFinanceKind): Promise<NextResponse> {
  const token = (await cookies()).get(sessionCookieName)?.value;
  if (!token) return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  try {
    const result = await aggregateEngineFinance(kind, token);
    return NextResponse.json(result.body, { status: result.status });
  } catch (error) {
    const failure = mapEngineFailure(error);
    return NextResponse.json(failure.body, { status: failure.status });
  }
}
