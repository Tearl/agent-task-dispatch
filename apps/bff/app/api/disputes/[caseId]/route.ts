import { disputeRoute } from "../../../../lib/dispute-route";
export async function GET(_request:Request,{params}:{params:Promise<{caseId:string}>}){return disputeRoute((await params).caseId);}
