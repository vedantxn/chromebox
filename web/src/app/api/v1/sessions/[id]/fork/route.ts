import { callerId, unauthorized, fromOrch } from '@/lib/caller';
import { orch, publicSession } from '@/lib/orch';

type OrchFork = { parent: string; paused_ms: number; total_ms: number; errors?: string[]; sessions: Parameters<typeof publicSession>[0][] };

// POST /api/v1/sessions/:id/fork {count}: copy the running browser, with its
// open tabs, cookies and storage, into count new sessions. Each copy has its
// own connectUrl and counts against your limits.
export async function POST(req: Request, { params }: { params: Promise<{ id: string }> }) {
  const uid = await callerId(req);
  if (!uid) return unauthorized();
  const body = await req.json().catch(() => ({}));
  const count = Number((body as { count?: number }).count ?? 1);
  const r = await orch<OrchFork>('POST', `/v1/users/${uid}/sessions/${(await params).id}/fork`, { count });
  if (!r.ok) return fromOrch(r);
  return Response.json({ parent: r.data.parent, pausedMs: r.data.paused_ms, totalMs: r.data.total_ms,
    sessions: r.data.sessions.map(publicSession), errors: r.data.errors ?? [] }, { status: 201 });
}
