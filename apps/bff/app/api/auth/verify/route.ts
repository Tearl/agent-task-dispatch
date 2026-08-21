import { NextResponse } from "next/server";
import { sessionCookie, splitSession } from "../../../../lib/session";
import { BodyTooLargeError, readBoundedBody } from "../../../../lib/body";

const engineBaseUrl = process.env.ENGINE_BASE_URL ?? "http://localhost:8080";

export async function POST(request: Request) {
  let body: string;
  try { body = await readBoundedBody(request, 16_384); }
  catch (error) { return NextResponse.json({ error: error instanceof BodyTooLargeError ? "request body too large" : "invalid request body" }, { status: error instanceof BodyTooLargeError ? 413 : 400 }); }
  const response = await fetch(`${engineBaseUrl}/v1/auth/verify`, { method: "POST", headers: { "content-type": "application/json" }, body, cache: "no-store" });
  if (!response.ok) return new NextResponse(response.body, { status: response.status, headers: { "content-type": "application/json" } });
  try {
    const { token, publicSession } = splitSession(await response.json());
    const result = NextResponse.json(publicSession, { status: 201 });
    result.cookies.set(sessionCookie(token, publicSession.expiresAt));
    return result;
  } catch {
    return NextResponse.json({ error: "invalid engine response" }, { status: 502 });
  }
}
