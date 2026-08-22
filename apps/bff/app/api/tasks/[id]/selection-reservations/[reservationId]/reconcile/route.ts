import { mutationRoute } from "../../../../../../../lib/mutation-route";

export async function POST(request: Request, { params }: { params: Promise<{ id: string; reservationId: string }> }) {
  const { id, reservationId } = await params;
  return mutationRoute(request, `/v1/tasks/${encodeURIComponent(id)}/selection-reservations/${encodeURIComponent(reservationId)}/reconcile`, 4_096);
}
