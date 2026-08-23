import { mutationRoute } from "../../../../../lib/mutation-route";
export async function POST(request:Request,{params}:{params:Promise<{caseId:string}>}){const{caseId}=await params;return mutationRoute(request,`/v1/disputes/${encodeURIComponent(caseId)}/reviews`);}
