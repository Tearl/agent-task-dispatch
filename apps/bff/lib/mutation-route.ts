import { cookies } from "next/headers";
import { NextResponse } from "next/server";
import { BodyTooLargeError, readBoundedBody } from "./body";
import { forwardEngineMutation, InvalidEngineResponseError, InvalidResourceIdError } from "./engine";
import { sessionCookieName } from "./session";

export async function mutationRoute(request: Request, path: string, bodyLimit = 131_072, engineMethod: "POST" | "PUT" = "POST"): Promise<NextResponse> {
  const token = (await cookies()).get(sessionCookieName)?.value;
  if (!token) return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  let body: string;
  try {
    body = await readBoundedBody(request, bodyLimit);
  } catch (error) {
    return NextResponse.json({ error: error instanceof BodyTooLargeError ? "request body too large" : "invalid request body" }, { status: error instanceof BodyTooLargeError ? 413 : 400 });
  }
  try {
    const result = await forwardEngineMutation({ path, body, idempotencyKey: request.headers.get("idempotency-key") ?? "", sessionToken: token, method: engineMethod });
    return NextResponse.json(result.body, { status: result.status });
  } catch (error) {
    if (error instanceof InvalidResourceIdError) return NextResponse.json({ error: "invalid resource id" }, { status: 400 });
    if (error instanceof InvalidEngineResponseError) return NextResponse.json({ error: "invalid engine response" }, { status: 502 });
    return NextResponse.json({ error: "engine service temporarily unavailable" }, { status: 503 });
  }
}
