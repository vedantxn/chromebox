import { headers } from 'next/headers';

// Server-side client for the orchestrator's internal API. The orchestrator
// token never leaves the server.
export type OrchResult<T> = { status: number; ok: boolean; data: T; error?: string; retryAfter?: string };

export async function orch<T = unknown>(method: string, path: string, body?: unknown): Promise<OrchResult<T>> {
  const hdrs: Record<string, string> = { Authorization: `Bearer ${process.env.ORCHESTRATOR_TOKEN}`, 'content-type': 'application/json' };
  if (path.startsWith('/v1/users/me/')) {
    // An API-key caller (see callerId): pass the key on, in a header, never in the URL.
    const h = (await headers()).get('authorization') ?? '';
    if (h.startsWith('Bearer ')) hdrs['X-Baton-Key'] = h.slice(7);
  }
  const res = await fetch(`${process.env.ORCHESTRATOR_URL}${path}`, {
    method,
    headers: hdrs,
    body: body === undefined ? undefined : JSON.stringify(body),
    cache: 'no-store',
  });
  const text = await res.text();
  let data: unknown = null;
  try { data = text ? JSON.parse(text) : null; } catch { /* plain-text error */ }
  return { status: res.status, ok: res.ok, data: data as T, error: res.ok ? undefined : text.trim(), retryAfter: res.headers.get('retry-after') ?? undefined };
}

// A session's options, as the orchestrator keeps them (snake_case).
type OrchOptions = {
  viewport?: { width: number; height: number }; user_agent?: string; locale?: string; timezone?: string;
  timeout_minutes?: number; proxy?: { server: string; username?: string; password?: string };
};

// The orchestrator's session shape, and the public one (camelCase).
type OrchSession = {
  id: string; status: string; node?: string; created_at: string; expires_at?: string;
  ended_at?: string; end_reason?: string; migrations: number; connect_url?: string; options?: OrchOptions;
};

// What POST /api/v1/sessions takes besides a profile, in the API's camelCase.
export type SessionOptions = {
  viewport?: { width: number; height: number }; userAgent?: string; locale?: string; timezone?: string;
  timeoutMinutes?: number; proxy?: { server: string; username?: string; password?: string };
};

export function orchOptions(o: SessionOptions): OrchOptions {
  return {
    viewport: o.viewport, user_agent: o.userAgent, locale: o.locale, timezone: o.timezone,
    timeout_minutes: o.timeoutMinutes, proxy: o.proxy,
  };
}

function publicOptions(o?: OrchOptions): SessionOptions | undefined {
  if (!o) return undefined;
  return {
    viewport: o.viewport, userAgent: o.user_agent, locale: o.locale, timezone: o.timezone,
    timeoutMinutes: o.timeout_minutes, proxy: o.proxy,
  };
}

export function publicSession(s: OrchSession) {
  return {
    id: s.id, status: s.status, node: s.node, createdAt: s.created_at, expiresAt: s.expires_at || undefined,
    endedAt: s.ended_at || undefined, endReason: s.end_reason || undefined, migrations: s.migrations,
    connectUrl: s.connect_url, options: publicOptions(s.options),
  };
}

// Timings go out in whole milliseconds and sizes to a tenth of a MiB: the
// orchestrator measures to the microsecond, which only clutters a client's log.
export const msOf = (v: number | undefined) => (v == null ? undefined : Math.round(v));
export const mibOf = (v: number | undefined) => (v == null ? undefined : Math.round(v * 10) / 10);
