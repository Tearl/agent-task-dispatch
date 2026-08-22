import { cookies } from "next/headers";
import { NextResponse } from "next/server";
import { aggregateEngineMatching, forwardEngineRead, InvalidEngineResponseError, InvalidResourceIdError } from "./engine";
import { sessionCookieName } from "./session";

async function respond(work: (token: string) => Promise<{ status: number; body: Record<string, unknown> }>) {
  const token = (await cookies()).get(sessionCookieName)?.value;
  if (!token) return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  try {
    const result = await work(token);
    return NextResponse.json(result.body, { status: result.status });
  } catch (error) {
    if (error instanceof InvalidResourceIdError) return NextResponse.json({ error: "invalid resource id" }, { status: 400 });
    if (error instanceof InvalidEngineResponseError) return NextResponse.json({ error: "invalid engine response" }, { status: 502 });
    return NextResponse.json({ error: "engine service temporarily unavailable" }, { status: 503 });
  }
}

export function matchingRoute(taskID: string) { return respond((token) => aggregateEngineMatching(taskID, token)); }
export function selectionReadRoute(taskID: string, reservationID: string) { return respond((token) => forwardEngineRead(`/v1/tasks/${encodeURIComponent(taskID)}/selection-reservations/${encodeURIComponent(reservationID)}`, token)); }
