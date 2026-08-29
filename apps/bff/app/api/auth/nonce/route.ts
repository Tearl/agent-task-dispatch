import { NextResponse } from "next/server";
import { BodyTooLargeError, readBoundedBody } from "../../../../lib/body";
import { mapEngineFailure, readEngineError, readEngineJSON, resolveEngineBaseUrl } from "../../../../lib/engine";
import { requestEngine } from "../../../../lib/engine-http";

export async function POST(request: Request) {
  let body: string;
  try { body = await readBoundedBody(request, 4_096); }
  catch (error) { return NextResponse.json({ error: error instanceof BodyTooLargeError ? "request body too large" : "invalid request body" }, { status: error instanceof BodyTooLargeError ? 413 : 400 }); }
  try {
    const response = await requestEngine(`${resolveEngineBaseUrl()}/v1/auth/nonce`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body,
      cache: "no-store",
    });
    const result = response.ok ? { status: response.status, body: await readEngineJSON(response) } : await readEngineError(response);
    return NextResponse.json(result.body, { status: result.status });
  } catch (error) {
    const failure = mapEngineFailure(error);
    return NextResponse.json(failure.body, { status: failure.status });
  }
}
