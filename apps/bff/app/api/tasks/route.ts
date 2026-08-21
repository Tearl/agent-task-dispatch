import { mutationRoute } from "../../../lib/mutation-route";

export async function POST(request: Request) { return mutationRoute(request, "/v1/tasks"); }
