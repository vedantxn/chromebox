import { callerId, unauthorized, fromOrch } from '@/lib/caller';
import { orch, publicSession } from '@/lib/orch';

type Ctx = { params: Promise<{ id: string }> };

export async function GET(req: Request, { params }: Ctx) {
  const uid = await callerId(req);
  if (!uid) return unauthorized();
  const r = await orch<Parameters<typeof publicSession>[0]>('GET', `/v1/users/${uid}/sessions/${(await params).id}`);
  return r.ok ? Response.json(publicSession(r.data)) : fromOrch(r);
}

// DELETE /api/v1/sessions/:id ends the session and destroys its browser.
export async function DELETE(req: Request, { params }: Ctx) {
  const uid = await callerId(req);
  if (!uid) return unauthorized();
  const r = await orch('DELETE', `/v1/users/${uid}/sessions/${(await params).id}`);
  return r.ok ? new Response(null, { status: 204 }) : fromOrch(r);
}
