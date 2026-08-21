import { cookies } from "next/headers";
import { NextResponse } from "next/server";
import { sessionCookieName, shouldClearSessionAfterLogout } from "../../../../lib/session";

const engineBaseUrl = process.env.ENGINE_BASE_URL ?? "http://localhost:8080";

async function token() { return (await cookies()).get(sessionCookieName)?.value; }

export async function GET() {
  const value = await token();
  if (!value) return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  const response = await fetch(`${engineBaseUrl}/v1/auth/session`, { headers: { authorization: `Bearer ${value}` }, cache: "no-store" });
  return new NextResponse(response.body, { status: response.status, headers: { "content-type": "application/json" } });
}

export async function DELETE() {
  const value = await token();
  if (value) {
    const response = await fetch(`${engineBaseUrl}/v1/auth/session`, { method: "DELETE", headers: { authorization: `Bearer ${value}` }, cache: "no-store" });
    if (!shouldClearSessionAfterLogout(response.status)) return new NextResponse(response.body, { status: response.status, headers: { "content-type": "application/json" } });
  }
  const result = new NextResponse(null, { status: 204 });
  result.cookies.set({ name: sessionCookieName, value: "", httpOnly: true, sameSite: "strict", secure: process.env.NODE_ENV === "production", path: "/", maxAge: 0 });
  return result;
}
