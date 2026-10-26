import { callerId, unauthorized, fromOrch } from '@/lib/caller';
import { orch, orchOptions, publicSession, type SessionOptions } from '@/lib/orch';

// GET /api/v1/sessions: your recent sessions.
export async function GET(req: Request) {
  const uid = await callerId(req);
  if (!uid) return unauthorized();
  const r = await orch<Parameters<typeof publicSession>[0][]>('GET', `/v1/users/${uid}/sessions`);
  return r.ok ? Response.json(r.data.map(publicSession)) : fromOrch(r);
}

// POST /api/v1/sessions: start a browser. Returns connectUrl for
// chromium.connectOverCDP(connectUrl). With {"profile": name} it starts from a
// saved profile: logged in, on the pages the profile was saved on. The other
// fields (SessionOptions) set how the browser presents itself, how long the
// session may run, and a proxy for its traffic; the orchestrator checks them.
export async function POST(req: Request) {
  const uid = await callerId(req);
  if (!uid) return unauthorized();
  const body = (await req.json().catch(() => ({}))) as { profile?: string } & SessionOptions;
  if (body === null || typeof body !== 'object' || Array.isArray(body)) return Response.json({ error: 'the body must be a JSON object' }, { status: 400 });
  const r = await orch<Parameters<typeof publicSession>[0]>('POST', `/v1/users/${uid}/sessions`, { profile: body.profile, ...orchOptions(body) });
  return r.ok ? Response.json(publicSession(r.data), { status: 201 }) : fromOrch(r);
}
