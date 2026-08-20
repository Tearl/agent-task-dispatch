import { NextResponse } from "next/server";

const engineBaseUrl = process.env.ENGINE_BASE_URL ?? "http://localhost:8080";

export async function GET() {
  try {
    const response = await fetch(`${engineBaseUrl}/healthz`, {
      cache: "no-store",
      signal: AbortSignal.timeout(2_000),
    });
    const engine = await response.json();

    return NextResponse.json({ status: "ok", service: "bff", engine });
  } catch {
    return NextResponse.json(
      { status: "degraded", service: "bff", engine: { status: "unavailable" } },
      { status: 503 },
    );
  }
}
