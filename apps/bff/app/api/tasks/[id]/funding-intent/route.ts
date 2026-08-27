import { cookies } from "next/headers";
import { NextResponse } from "next/server";
import { forwardEngineRead, InvalidEngineResponseError, InvalidResourceIdError } from "../../../../../lib/engine";
import { sessionCookieName } from "../../../../../lib/session";

export async function GET(_request: Request, { params }: { params: Promise<{ id: string }> }) {
  const token = (await cookies()).get(sessionCookieName)?.value;
  if (!token) return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  const { id } = await params;
  try {
    const result = await forwardEngineRead(`/v1/tasks/${encodeURIComponent(id)}/funding-intent`, token);
    return NextResponse.json(result.body, { status: result.status });
  } catch (error) {
    if (error instanceof InvalidResourceIdError) return NextResponse.json({ error: "invalid resource id" }, { status: 400 });
    if (error instanceof InvalidEngineResponseError) return NextResponse.json({ error: "invalid engine response" }, { status: 502 });
    return NextResponse.json({ error: "engine service temporarily unavailable" }, { status: 503 });
  }
}

