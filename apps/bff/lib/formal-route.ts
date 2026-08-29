import { cookies } from "next/headers";
import { NextResponse } from "next/server";
import { aggregateEngineFormalDelivery, InvalidResourceIdError, mapEngineFailure } from "./engine";
import { sessionCookieName } from "./session";

export async function formalDeliveryRoute(taskID: string) {
  const token = (await cookies()).get(sessionCookieName)?.value;
  if (!token) return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  try {
    const result = await aggregateEngineFormalDelivery(taskID, token);
    return NextResponse.json(result.body, { status: result.status });
  } catch (error) {
    if (error instanceof InvalidResourceIdError) return NextResponse.json({ error: "invalid resource id" }, { status: 400 });
    const failure = mapEngineFailure(error);
    return NextResponse.json(failure.body, { status: failure.status });
  }
}
