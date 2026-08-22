import { selectionReadRoute } from "../../../../../../lib/matching-route";

export async function GET(_request: Request, { params }: { params: Promise<{ id: string; reservationId: string }> }) {
  const { id, reservationId } = await params;
  return selectionReadRoute(id, reservationId);
}
