import { callerId, unauthorized, fromOrch } from '@/lib/caller';
import { orch } from '@/lib/orch';

// GET /api/v1/sessions/:id/events: what happened to the session, oldest first
// (created, migrated with its pause, forked, ended and why, ...).
export async function GET(req: Request, { params }: { params: Promise<{ id: string }> }) {
  const uid = await callerId(req);
  if (!uid) return unauthorized();
  const r = await orch<{ at: string; kind: string; detail?: Record<string, unknown> }[]>(
    'GET', `/v1/users/${uid}/sessions/${(await params).id}/events`);
  return r.ok ? Response.json(r.data) : fromOrch(r);
}
