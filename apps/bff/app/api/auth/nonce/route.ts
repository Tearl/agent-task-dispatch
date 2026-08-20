import { NextResponse } from "next/server";

const engineBaseUrl = process.env.ENGINE_BASE_URL ?? "http://localhost:8080";

export async function POST(request: Request) {
  const body = await request.text();
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
