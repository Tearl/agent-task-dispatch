import { mutationRoute } from "../../../../../../../../../lib/mutation-route";

export async function POST(request: Request, { params }: { params: Promise<{ id: string; batchId: string; slotId: string }> }) {
  const { id, batchId, slotId } = await params;
  return mutationRoute(request, `/v1/tasks/${encodeURIComponent(id)}/overview-batches/${encodeURIComponent(batchId)}/slots/${encodeURIComponent(slotId)}/finalize`, 1_024);
}
