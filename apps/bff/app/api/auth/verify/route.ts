import { NextResponse } from "next/server";
import { sessionCookie, splitSession } from "../../../../lib/session";
import { BodyTooLargeError, readBoundedBody } from "../../../../lib/body";
import { InvalidEngineResponseError, mapEngineFailure, readEngineError, readEngineJSON, resolveEngineBaseUrl } from "../../../../lib/engine";
import { requestEngine } from "../../../../lib/engine-http";

export async function POST(request: Request) {
  let body: string;
  try { body = await readBoundedBody(request, 16_384); }
  catch (error) { return NextResponse.json({ error: error instanceof BodyTooLargeError ? "request body too large" : "invalid request body" }, { status: error instanceof BodyTooLargeError ? 413 : 400 }); }
  try {
    const response = await requestEngine(`${resolveEngineBaseUrl()}/v1/auth/verify`, { method: "POST", headers: { "content-type": "application/json" }, body, cache: "no-store" });
    if (!response.ok) {
      const failure = await readEngineError(response);
      return NextResponse.json(failure.body, { status: failure.status });
    }
    let authenticated;
    try {
      authenticated = splitSession(await readEngineJSON(response));
    } catch (error) {
      if (error instanceof InvalidEngineResponseError) throw error;
      throw new InvalidEngineResponseError("invalid Engine authentication response");
    }
    const { token, publicSession } = authenticated;
    const result = NextResponse.json(publicSession, { status: 201 });
    result.cookies.set(sessionCookie(token, publicSession.expiresAt));
    return result;
  } catch (error) {
    const failure = mapEngineFailure(error);
    return NextResponse.json(failure.body, { status: failure.status });
  }
}
