import { mutationRoute } from "../../../../../lib/mutation-route";

export async function POST(request: Request, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return mutationRoute(request, `/v1/tasks/${encodeURIComponent(id)}/formal-feedback`, 512_000);
}
