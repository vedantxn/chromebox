// Server-side client for the orchestrator's internal API. The orchestrator
// token never leaves the server.
export type OrchResult<T> = { status: number; ok: boolean; data: T; error?: string };

export async function orch<T = unknown>(method: string, path: string, body?: unknown): Promise<OrchResult<T>> {
  const res = await fetch(`${process.env.ORCHESTRATOR_URL}${path}`, {
    method,
    headers: { Authorization: `Bearer ${process.env.ORCHESTRATOR_TOKEN}`, 'content-type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
    cache: 'no-store',
  });
  const text = await res.text();
  let data: unknown = null;
  try { data = text ? JSON.parse(text) : null; } catch { /* plain-text error */ }
  return { status: res.status, ok: res.ok, data: data as T, error: res.ok ? undefined : text.trim() };
}

// The orchestrator's session shape, and the public one (camelCase).
type OrchSession = {
  id: string; status: string; node?: string; created_at: string; expires_at?: string;
  ended_at?: string; end_reason?: string; migrations: number; connect_url?: string;
};

export function publicSession(s: OrchSession) {
  return {
    id: s.id, status: s.status, node: s.node, createdAt: s.created_at, expiresAt: s.expires_at || undefined,
    endedAt: s.ended_at || undefined, endReason: s.end_reason || undefined, migrations: s.migrations,
    connectUrl: s.connect_url,
  };
}
