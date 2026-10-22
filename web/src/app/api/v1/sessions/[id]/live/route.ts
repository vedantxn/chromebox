import { callerId, unauthorized, fromOrch } from '@/lib/caller';
import { orch } from '@/lib/orch';

// POST /api/v1/sessions/:id/live: a short-lived URL for watching the browser.
export async function POST(req: Request, { params }: { params: Promise<{ id: string }> }) {
  const uid = await callerId(req);
  if (!uid) return unauthorized();
  const r = await orch<{ url: string }>('POST', `/v1/users/${uid}/sessions/${(await params).id}/token?kind=view`);
  return r.ok ? Response.json({ url: r.data.url }) : fromOrch(r);
}
