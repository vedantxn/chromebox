import { callerId, unauthorized, fromOrch } from '@/lib/caller';
import { orch, publicSession } from '@/lib/orch';

// GET /api/v1/sessions: your recent sessions.
export async function GET(req: Request) {
  const uid = await callerId(req);
  if (!uid) return unauthorized();
  const r = await orch<Parameters<typeof publicSession>[0][]>('GET', `/v1/users/${uid}/sessions`);
  return r.ok ? Response.json(r.data.map(publicSession)) : fromOrch(r);
}

// POST /api/v1/sessions: start a browser. Returns connectUrl for
// chromium.connectOverCDP(connectUrl). With {"profile": name} it starts from a
// saved profile: logged in, on the pages the profile was saved on.
export async function POST(req: Request) {
  const uid = await callerId(req);
  if (!uid) return unauthorized();
  const body = (await req.json().catch(() => ({}))) as { profile?: string };
  const r = await orch<Parameters<typeof publicSession>[0]>('POST', `/v1/users/${uid}/sessions`, body.profile ? { profile: body.profile } : undefined);
  return r.ok ? Response.json(publicSession(r.data), { status: 201 }) : fromOrch(r);
}
