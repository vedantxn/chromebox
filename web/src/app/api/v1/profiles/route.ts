import { callerId, unauthorized, fromOrch } from '@/lib/caller';
import { mibOf, msOf, orch } from '@/lib/orch';

type Prof = { name: string; created_at: string; stored_mib: number };

// GET /api/v1/profiles: your saved profiles.
export async function GET(req: Request) {
  const uid = await callerId(req);
  if (!uid) return unauthorized();
  const r = await orch<Prof[]>('GET', `/v1/users/${uid}/profiles`);
  return r.ok ? Response.json(r.data.map((p) => ({ name: p.name, createdAt: p.created_at, storedMiB: mibOf(p.stored_mib) }))) : fromOrch(r);
}

// POST /api/v1/profiles {session, name}: save a running session's state (pages,
// cookies, storage) as a profile. The session keeps running. Start new sessions
// from it with POST /api/v1/sessions {"profile": name}: they begin logged in.
export async function POST(req: Request) {
  const uid = await callerId(req);
  if (!uid) return unauthorized();
  const body = (await req.json().catch(() => ({}))) as { session?: string; name?: string };
  const r = await orch<{ name: string; paused_ms: number; upload_ms: number; stored_mib: number }>('POST', `/v1/users/${uid}/profiles`, body);
  if (!r.ok) return fromOrch(r);
  return Response.json({ name: r.data.name, pausedMs: msOf(r.data.paused_ms), uploadMs: msOf(r.data.upload_ms), storedMiB: mibOf(r.data.stored_mib) }, { status: 201 });
}
