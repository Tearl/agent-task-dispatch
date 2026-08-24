import { executionsRoute } from "../../../../../lib/matching-route";

export async function GET(_request: Request, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return executionsRoute(id);
}
