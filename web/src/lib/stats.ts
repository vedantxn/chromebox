import { cacheLife } from 'next/cache';

// The control plane's live numbers, for the public status page. Server side
// only (it carries the orchestrator token). Cached for 30 s, so a busy page
// cannot become load on the orchestrator.
export type Pct = { n: number; p50_ms: number; p95_ms: number };
export type StatsNode = { name: string; up: boolean; cordoned: boolean; state?: string; vms: number; cap_mib: number; used_mib: number };
export type Stats = {
  at: string;
  nodes: StatsNode[];
  sessions: { running?: number; paused?: number };
  last_hour: Record<string, Pct>;
  failures_last_hour: Record<string, number>;
  autoscaler: boolean;
  autoscaler_actions: { at: string; kind: string; node: string; reason: string }[] | null;
};

export async function getStats(): Promise<Stats | null> {
  'use cache';
  cacheLife({ stale: 30, revalidate: 30, expire: 600 }); // shared by every visitor
  try {
    const res = await fetch(`${process.env.ORCHESTRATOR_URL}/v1/stats`, {
      headers: { Authorization: `Bearer ${process.env.ORCHESTRATOR_TOKEN}` },
      signal: AbortSignal.timeout(5000),
    });
    return res.ok ? ((await res.json()) as Stats) : null;
  } catch {
    return null;
  }
}

// operational: the always-on machine is up and few operations failed in the last hour.
export function health(s: Stats | null): { label: string; ok: boolean } {
  if (!s) return { label: 'The control plane is not answering', ok: false };
  const first = s.nodes.find((n) => n.name === 'node-a') ?? s.nodes[0];
  if (!first?.up) return { label: 'The main machine is down', ok: false };
  const done = Object.values(s.last_hour).reduce((a, p) => a + p.n, 0);
  const failed = Object.values(s.failures_last_hour).reduce((a, n) => a + n, 0);
  if (failed > 0 && failed / Math.max(done, 1) > 0.05) return { label: 'Some operations are failing', ok: false };
  return { label: 'All systems operational', ok: true };
}
