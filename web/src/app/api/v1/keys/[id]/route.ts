import { dashboardUser } from '@/lib/caller';
import { orch } from '@/lib/orch';

export async function DELETE(_req: Request, { params }: { params: Promise<{ id: string }> }) {
  const uid = await dashboardUser();
  if (!uid) return Response.json({ error: 'sign in first' }, { status: 401 });
  await orch('DELETE', `/v1/users/${uid}/keys/${(await params).id}`);
  return new Response(null, { status: 204 });
}
