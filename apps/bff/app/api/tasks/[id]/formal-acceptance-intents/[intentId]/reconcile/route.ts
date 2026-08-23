import { mutationRoute } from "../../../../../../../lib/mutation-route";

export async function POST(request: Request, { params }: { params: Promise<{ id: string; intentId: string }> }) {
  const { id, intentId } = await params;
  return mutationRoute(request, `/v1/tasks/${encodeURIComponent(id)}/formal-acceptance-intents/${encodeURIComponent(intentId)}/reconcile`, 8_192);
}
