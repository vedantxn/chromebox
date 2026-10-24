import { callerId, unauthorized, fromOrch } from '@/lib/caller';
import { orch } from '@/lib/orch';

// DELETE /api/v1/profiles/:name
export async function DELETE(req: Request, { params }: { params: Promise<{ name: string }> }) {
  const uid = await callerId(req);
  if (!uid) return unauthorized();
  const r = await orch('DELETE', `/v1/users/${uid}/profiles/${encodeURIComponent((await params).name)}`);
  return r.ok ? new Response(null, { status: 204 }) : fromOrch(r);
}
