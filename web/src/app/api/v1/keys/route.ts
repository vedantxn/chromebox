import { dashboardUser, fromOrch } from '@/lib/caller';
import { orch } from '@/lib/orch';

const signIn = () => Response.json({ error: 'sign in first' }, { status: 401 });

// API keys are managed from the dashboard only (session cookie), never with a key.
export async function GET() {
  const uid = await dashboardUser();
  if (!uid) return signIn();
  const r = await orch('GET', `/v1/users/${uid}/keys`);
  return r.ok ? Response.json(r.data) : fromOrch(r);
}

export async function POST(req: Request) {
  const uid = await dashboardUser();
  if (!uid) return signIn();
  const { name } = await req.json().catch(() => ({ name: '' }));
  const r = await orch('POST', `/v1/users/${uid}/keys`, { name });
  return r.ok ? Response.json(r.data, { status: 201 }) : fromOrch(r);
}
