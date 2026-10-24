import { auth } from '@/auth';
import { orch } from '@/lib/orch';

// Who is calling: an API key (`Authorization: Bearer bk_...`, for scripts and
// the SDK) or a signed-in dashboard user (session cookie).
export async function callerId(req: Request): Promise<string | null> {
  const h = req.headers.get('authorization') ?? '';
  if (h.startsWith('Bearer ')) {
    const r = await orch<{ user_id: string }>('POST', '/v1/keys/verify', { key: h.slice(7) });
    return r.ok ? r.data.user_id : null;
  }
  const s = await auth();
  return s?.uid ?? null;
}

export async function dashboardUser(): Promise<string | null> {
  const s = await auth();
  return s?.uid ?? null;
}

export const unauthorized = () =>
  Response.json({ error: 'Missing or invalid API key. Send Authorization: Bearer <your key>.' }, { status: 401 });

// Errors from the orchestrator keep their status; a refusal for capacity while
// a machine is starting also keeps its Retry-After, which the SDK honours.
export const fromOrch = (r: { status: number; error?: string; retryAfter?: string }) =>
  Response.json({ error: r.error ?? 'error' }, { status: r.status, headers: r.retryAfter ? { 'Retry-After': r.retryAfter } : undefined });
