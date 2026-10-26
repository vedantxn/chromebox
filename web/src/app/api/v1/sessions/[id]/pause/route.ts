import { callerId, unauthorized, fromOrch } from '@/lib/caller';
import { mibOf, msOf, orch } from '@/lib/orch';

// POST /api/v1/sessions/:id/pause: store the running browser, with its pages,
// cookies and memory, and free its machine. Idle browsers cost nothing; resume
// it later, on any machine. Open connections are closed.
export async function POST(req: Request, { params }: { params: Promise<{ id: string }> }) {
  const uid = await callerId(req);
  if (!uid) return unauthorized();
  const r = await orch<{ pause: { paused_ms: number; upload_ms: number; total_ms: number; stored_mib: number } }>(
    'POST', `/v1/users/${uid}/sessions/${(await params).id}/pause`);
  if (!r.ok) return fromOrch(r);
  const p = r.data.pause;
  return Response.json({ status: 'paused', pausedMs: msOf(p.paused_ms), uploadMs: msOf(p.upload_ms), totalMs: msOf(p.total_ms), storedMiB: mibOf(p.stored_mib) });
}
