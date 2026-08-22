import { mutationRoute } from "../../../../../../../lib/mutation-route";

export async function POST(request: Request, { params }: { params: Promise<{ id: string; changeOrderId: string }> }) {
  const { id, changeOrderId } = await params;
  return mutationRoute(request, `/v1/tasks/${encodeURIComponent(id)}/formal-change-orders/${encodeURIComponent(changeOrderId)}/accept`, 8_192);
}
