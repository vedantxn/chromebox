import { callerId, unauthorized, fromOrch } from '@/lib/caller';
import { msOf, orch, publicSession } from '@/lib/orch';

// POST /api/v1/sessions/:id/resume: bring a paused browser back, exactly as it
// was, on any machine. Returns the session with a fresh connectUrl.
export async function POST(req: Request, { params }: { params: Promise<{ id: string }> }) {
  const uid = await callerId(req);
  if (!uid) return unauthorized();
  const r = await orch<{ session: Parameters<typeof publicSession>[0]; resume: { total_ms: number; download_ms: number } }>(
    'POST', `/v1/users/${uid}/sessions/${(await params).id}/resume`);
  if (!r.ok) return fromOrch(r);
  return Response.json({ ...publicSession(r.data.session), resumeMs: msOf(r.data.resume.total_ms) });
}
