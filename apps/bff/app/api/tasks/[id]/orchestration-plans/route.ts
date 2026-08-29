import { mutationRoute } from "../../../../../lib/mutation-route";

export async function POST(request: Request, context: { params: Promise<{ id: string }> }) {
  const { id } = await context.params;
  return mutationRoute(request, `/v1/tasks/${encodeURIComponent(id)}/orchestration-plans`, 1_024, "POST", 15_000);
}
