import { mutationRoute } from "../../../../../lib/mutation-route";

export async function POST(request: Request, context: { params: Promise<{ id: string }> }) {
  const { id } = await context.params;
  return mutationRoute(request, `/v1/tasks/${encodeURIComponent(id)}/deletion-requests`, 4_096);
}
