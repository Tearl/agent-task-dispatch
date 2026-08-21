import { resourceRoute } from "../../../../lib/resource-route";

export async function GET(_request: Request, context: { params: Promise<{ id: string }> }) {
  const { id } = await context.params;
  return resourceRoute("tasks", id);
}
