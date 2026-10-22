import { callerId, unauthorized } from '@/lib/caller';
import { orch } from '@/lib/orch';

// POST /api/v1/sessions/:id/migrate moves the running browser to another
// machine; connections stay open.
export async function POST(req: Request, { params }: { params: Promise<{ id: string }> }) {
  const uid = await callerId(req);
  if (!uid) return unauthorized();
  const r = await orch<{ from: string; to: string; paused_ms: number; unreachable_ms: number; stream_ms?: number; stream_mib?: number; error?: string }>(
    'POST', `/v1/users/${uid}/sessions/${(await params).id}/migrate`);
  if (!r.data) return Response.json({ error: r.error }, { status: r.status });
  const d = r.data;
  return Response.json({ from: d.from, to: d.to, pausedMs: d.paused_ms, unreachableMs: d.unreachable_ms,
    streamMs: d.stream_ms, streamMiB: d.stream_mib, error: d.error },
    { status: r.status });
}
