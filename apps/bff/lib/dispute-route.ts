import { cookies } from "next/headers";
import { NextResponse } from "next/server";
import { aggregateEngineDisputes, InvalidResourceIdError, mapEngineFailure } from "./engine";
import { sessionCookieName } from "./session";

export async function disputeRoute(caseID?: string) {
  const token=(await cookies()).get(sessionCookieName)?.value;
  if(!token)return NextResponse.json({error:"unauthorized"},{status:401});
  try{const result=await aggregateEngineDisputes(caseID,token);return NextResponse.json(result.body,{status:result.status});}
  catch(error){if(error instanceof InvalidResourceIdError)return NextResponse.json({error:"invalid dispute id"},{status:400});const failure=mapEngineFailure(error);return NextResponse.json(failure.body,{status:failure.status});}
}
