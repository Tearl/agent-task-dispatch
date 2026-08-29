import { NextResponse } from "next/server";
import { mapEngineFailure, readEngineError, readEngineJSON, resolveEngineBaseUrl } from "../../../lib/engine";
import { requestEngine } from "../../../lib/engine-http";

export async function GET() {
  try {
    const response = await requestEngine(`${resolveEngineBaseUrl()}/healthz`, { cache: "no-store" });
    if (!response.ok) {
      const failure = await readEngineError(response);
      return NextResponse.json(failure.body, { status: failure.status });
    }
    const engine = await readEngineJSON(response);

    return NextResponse.json({ status: "ok", service: "bff", engine });
  } catch (error) {
    const failure = mapEngineFailure(error);
    return NextResponse.json(failure.body, { status: failure.status });
  }
}
