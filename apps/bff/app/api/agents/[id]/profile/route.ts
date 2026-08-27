import { mutationRoute } from "../../../../../lib/mutation-route";

export async function PUT(request: Request, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return mutationRoute(request, `/v1/agents/${encodeURIComponent(id)}/profile`, 32_768, "PUT");
}
