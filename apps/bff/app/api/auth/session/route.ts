import { cookies } from "next/headers";
import { NextResponse } from "next/server";
import { parsePublicSession, sessionCookieName, shouldClearSessionAfterLogout } from "../../../../lib/session";
import { InvalidEngineResponseError, mapEngineFailure, readEngineError, readEngineJSON, resolveEngineBaseUrl } from "../../../../lib/engine";
import { requestEngine } from "../../../../lib/engine-http";

async function token() { return (await cookies()).get(sessionCookieName)?.value; }

export async function GET() {
  const value = await token();
  if (!value) return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  try {
    const response = await requestEngine(`${resolveEngineBaseUrl()}/v1/auth/session`, { headers: { authorization: `Bearer ${value}` }, cache: "no-store" });
    if (!response.ok) {
      const failure = await readEngineError(response);
      return NextResponse.json(failure.body, { status: failure.status });
    }
    try {
      return NextResponse.json(parsePublicSession(await readEngineJSON(response)), { status: response.status });
    } catch (error) {
      if (error instanceof InvalidEngineResponseError) throw error;
      throw new InvalidEngineResponseError("invalid Engine session response");
    }
  } catch (error) {
    const failure = mapEngineFailure(error);
    return NextResponse.json(failure.body, { status: failure.status });
  }
}

export async function DELETE() {
  const value = await token();
  if (value) {
    try {
      const response = await requestEngine(`${resolveEngineBaseUrl()}/v1/auth/session`, { method: "DELETE", headers: { authorization: `Bearer ${value}` }, cache: "no-store" });
      if (!shouldClearSessionAfterLogout(response.status)) {
        const failure = await readEngineError(response);
        return NextResponse.json(failure.body, { status: failure.status });
      }
    } catch (error) {
      const failure = mapEngineFailure(error);
      return NextResponse.json(failure.body, { status: failure.status });
    }
  }
  const result = new NextResponse(null, { status: 204 });
  result.cookies.set({ name: sessionCookieName, value: "", httpOnly: true, sameSite: "strict", secure: process.env.NODE_ENV === "production", path: "/", maxAge: 0 });
  return result;
}
