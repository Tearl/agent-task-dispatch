import { formalDeliveryRoute } from "../../../../../lib/formal-route";

export async function GET(_request: Request, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return formalDeliveryRoute(id);
}
