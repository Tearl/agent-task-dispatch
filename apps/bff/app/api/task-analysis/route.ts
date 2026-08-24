import { cookies } from "next/headers";
import { NextResponse } from "next/server";
import { BodyTooLargeError, readBoundedBody } from "../../../lib/body";
import { resolveEngineBaseUrl } from "../../../lib/engine";
import { sessionCookieName, type PublicSession } from "../../../lib/session";
import {
  generateTaskAnalysis,
  InvalidTaskAnalysisInputError,
  InvalidTaskAnalysisResponseError,
  parseTaskAnalysisInput,
  TaskAnalysisProviderError,
} from "../../../lib/task-analysis";

export async function POST(request: Request) {
  const token = (await cookies()).get(sessionCookieName)?.value;
  if (!token) return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  const authorization = await authorizePublisher(token);
  if (authorization) return authorization;

  let input: unknown;
  try {
    input = JSON.parse(await readBoundedBody(request, 65_536));
    const result = await generateTaskAnalysis(parseTaskAnalysisInput(input));
    return NextResponse.json(result, { status: 200 });
  } catch (error) {
    if (error instanceof BodyTooLargeError) return NextResponse.json({ error: "request body too large" }, { status: 413 });
    if (error instanceof InvalidTaskAnalysisInputError || error instanceof SyntaxError) return NextResponse.json({ error: "invalid task analysis request" }, { status: 400 });
    if (error instanceof InvalidTaskAnalysisResponseError) return NextResponse.json({ error: "model returned invalid task analysis" }, { status: 502 });
    if (error instanceof TaskAnalysisProviderError) return NextResponse.json({ error: "task analysis provider temporarily unavailable" }, { status: 503 });
    return NextResponse.json({ error: "task analysis is not configured" }, { status: 503 });
  }
}

async function authorizePublisher(token: string): Promise<NextResponse | null> {
  try {
    const response = await fetch(`${resolveEngineBaseUrl()}/v1/auth/session`, {
      headers: { authorization: `Bearer ${token}`, accept: "application/json" },
      cache: "no-store",
      signal: AbortSignal.timeout(3_000),
    });
    if (response.status === 401) return NextResponse.json({ error: "unauthorized" }, { status: 401 });
    if (!response.ok) return NextResponse.json({ error: "authorization service temporarily unavailable" }, { status: 503 });
    const session = await response.json() as PublicSession;
    if (!Array.isArray(session.roles) || !session.roles.includes("publisher")) return NextResponse.json({ error: "forbidden" }, { status: 403 });
    return null;
  } catch {
    return NextResponse.json({ error: "authorization service temporarily unavailable" }, { status: 503 });
  }
}
