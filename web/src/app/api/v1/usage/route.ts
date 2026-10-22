import { callerId, unauthorized, fromOrch } from '@/lib/caller';
import { orch } from '@/lib/orch';

export async function GET(req: Request) {
  const uid = await callerId(req);
  if (!uid) return unauthorized();
  const r = await orch('GET', `/v1/users/${uid}/usage`);
  return r.ok ? Response.json(r.data) : fromOrch(r);
}
