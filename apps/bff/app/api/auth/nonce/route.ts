import { NextResponse } from "next/server";
import { BodyTooLargeError, readBoundedBody } from "../../../../lib/body";

const engineBaseUrl = process.env.ENGINE_BASE_URL ?? "http://localhost:8080";

export async function POST(request: Request) {
  let body: string;
  try { body = await readBoundedBody(request, 4_096); }
  catch (error) { return NextResponse.json({ error: error instanceof BodyTooLargeError ? "request body too large" : "invalid request body" }, { status: error instanceof BodyTooLargeError ? 413 : 400 }); }
  const response = await fetch(`${engineBaseUrl}/v1/auth/nonce`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body,
    cache: "no-store",
  });

  return new NextResponse(response.body, {
    status: response.status,
    headers: { "content-type": "application/json" },
  });
}
