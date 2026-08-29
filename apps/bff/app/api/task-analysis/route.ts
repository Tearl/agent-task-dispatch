import { cookies } from "next/headers";
import { NextResponse } from "next/server";
import { BodyTooLargeError, readBoundedBody } from "../../../lib/body";
import { InvalidEngineResponseError, mapEngineFailure, readEngineError, readEngineJSON, resolveEngineBaseUrl } from "../../../lib/engine";
import { requestEngine } from "../../../lib/engine-http";
import { sessionCookieName, type PublicSession } from "../../../lib/session";
import {
  generateTaskAnalysis,
  generateLocalTaskAnalysis,
  InvalidTaskAnalysisInputError,
  InvalidTaskAnalysisResponseError,
  parseTaskAnalysisInput,
  TaskAnalysisConfigurationError,
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
    const parsed = parseTaskAnalysisInput(input);
    let result;
    try {
      result = await generateTaskAnalysis(parsed);
    } catch (error) {
      if (!(error instanceof TaskAnalysisConfigurationError) || process.env.NODE_ENV === "production") throw error;
      result = generateLocalTaskAnalysis(parsed);
    }
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
    const response = await requestEngine(`${resolveEngineBaseUrl()}/v1/auth/session`, {
      headers: { authorization: `Bearer ${token}`, accept: "application/json" },
      cache: "no-store",
    });
    if (!response.ok) {
      const failure = await readEngineError(response);
      return NextResponse.json(failure.body, { status: failure.status });
    }
    const session = await readEngineJSON(response) as Partial<PublicSession> | null;
    if (!session || !Array.isArray(session.roles) || !session.roles.every((role) => typeof role === "string")) {
      throw new InvalidEngineResponseError("invalid Engine authorization response");
    }
    if (!session.roles.includes("publisher")) return NextResponse.json({ error: "forbidden" }, { status: 403 });
    return null;
  } catch (error) {
    const failure = mapEngineFailure(error);
    return NextResponse.json(failure.body, { status: failure.status });
  }
}
