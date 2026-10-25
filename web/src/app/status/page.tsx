import Link from 'next/link';
import { Suspense } from 'react';
import { getStats, health } from '@/lib/stats';

export const metadata = { title: 'Status' };

const ms = (v: number) => (v >= 1000 ? `${(v / 1000).toFixed(1)} s` : `${Math.round(v)} ms`);
const mib = (v: number) => `${(v / 1024).toFixed(1)} GiB`;

const ROWS: [string, string][] = [
  ['create', 'Start a browser'],
  ['migration_pause', 'Live migration: browser paused'],
  ['fork_parent_pause', 'Fork: browser paused'],
  ['pause', 'Pause: stored to disk'],
  ['resume', 'Resume: back from disk'],
];

function nodeState(n: { up: boolean; cordoned: boolean; state?: string }) {
  if (n.state) return n.state;
  if (!n.up) return 'stopped';
  return n.cordoned ? 'closed to new browsers' : 'running';
}

async function Body() {
  const s = await getStats();
  const h = health(s);
  const up = s?.nodes.filter((n) => n.up).length ?? 0;
  return (
    <>
      <p className="mt-3 flex items-center gap-2 text-lg">
        <span className={`inline-block h-3 w-3 rounded-full ${h.ok ? 'bg-emerald-500' : 'bg-red-500'}`} aria-hidden />
        {h.label}
      </p>
      {s && <p className="mt-1 text-sm text-zinc-500">Updated {new Date(s.at).toUTCString().slice(17, 25)} UTC · refreshes every 30 s · <a className="underline" href="/api/status">JSON</a></p>}

      {s && (
        <>
          <div className="mt-8 grid gap-3 sm:grid-cols-3">
            <div className="rounded-lg border border-zinc-200 p-4 dark:border-zinc-800"><p className="text-3xl font-semibold tabular-nums">{up}<span className="text-lg text-zinc-500"> / {s.nodes.length}</span></p><p className="mt-1 text-sm">machines up</p></div>
            <div className="rounded-lg border border-zinc-200 p-4 dark:border-zinc-800"><p className="text-3xl font-semibold tabular-nums">{s.sessions.running ?? 0}</p><p className="mt-1 text-sm">browsers running</p></div>
            <div className="rounded-lg border border-zinc-200 p-4 dark:border-zinc-800"><p className="text-3xl font-semibold tabular-nums">{s.sessions.paused ?? 0}</p><p className="mt-1 text-sm">paused (using no machine)</p></div>
          </div>

          <h2 className="mt-10 text-lg font-semibold">Machines</h2>
          <div className="mt-3 overflow-x-auto">
            <table className="w-full min-w-[480px] text-left text-sm">
              <thead className="text-zinc-500"><tr><th className="pb-2 pr-4 font-normal">machine</th><th className="pb-2 pr-4 font-normal">state</th><th className="pb-2 pr-4 font-normal">browsers</th><th className="pb-2 font-normal">memory in use</th></tr></thead>
              <tbody>{s.nodes.map((n) => (
                <tr key={n.name} className="border-t border-zinc-200 dark:border-zinc-800">
                  <td className="py-2 pr-4 font-mono">{n.name}</td>
                  <td className="py-2 pr-4">{nodeState(n)}</td>
                  <td className="py-2 pr-4 tabular-nums">{n.up ? n.vms : '–'}</td>
                  <td className="py-2">{n.up && n.cap_mib > 0 ? (
                    <span className="flex items-center gap-2">
                      <span className="inline-block h-2 w-24 overflow-hidden rounded bg-zinc-200 dark:bg-zinc-800"><span className="block h-full bg-amber-500" style={{ width: `${Math.min(100, (100 * n.used_mib) / n.cap_mib)}%` }} /></span>
                      <span className="tabular-nums text-zinc-500">{mib(n.used_mib)} of {mib(n.cap_mib)}</span>
                    </span>) : '–'}</td>
                </tr>))}
              </tbody>
            </table>
          </div>

          <h2 className="mt-10 text-lg font-semibold">The last hour</h2>
          <div className="mt-3 overflow-x-auto">
            <table className="w-full min-w-[480px] text-left text-sm">
              <thead className="text-zinc-500"><tr><th className="pb-2 pr-4 font-normal"></th><th className="pb-2 pr-4 font-normal">count</th><th className="pb-2 pr-4 font-normal">p50</th><th className="pb-2 font-normal">p95</th></tr></thead>
              <tbody>{ROWS.map(([k, label]) => { const p = s.last_hour[k]; return (
                <tr key={k} className="border-t border-zinc-200 dark:border-zinc-800">
                  <td className="py-2 pr-4">{label}</td>
                  <td className="py-2 pr-4 tabular-nums">{p?.n ?? 0}</td>
                  <td className="py-2 pr-4 tabular-nums">{p?.n ? ms(p.p50_ms) : '–'}</td>
                  <td className="py-2 tabular-nums">{p?.n ? ms(p.p95_ms) : '–'}</td>
                </tr>); })}
              </tbody>
            </table>
          </div>
          {Object.keys(s.failures_last_hour).length > 0 && (
            <p className="mt-3 text-sm text-red-600">Failures: {Object.entries(s.failures_last_hour).map(([k, n]) => `${k.replace(/_/g, ' ')} ×${n}`).join(', ')}</p>
          )}

          {s.autoscaler && (
            <>
              <h2 className="mt-10 text-lg font-semibold">Capacity</h2>
              <p className="mt-1 text-sm text-zinc-600 dark:text-zinc-400">
                A machine starts when the others are nearly full. When load drops, its browsers are moved to the others (live, with no disconnects) and it is stopped.
              </p>
              <ul className="mt-3 space-y-1 text-sm">
                {(s.autoscaler_actions ?? []).length === 0 && <li className="text-zinc-500">No changes lately.</li>}
                {[...(s.autoscaler_actions ?? [])].reverse().map((a, i) => (
                  <li key={i}><span className="text-zinc-500">{new Date(a.at).toUTCString().slice(17, 25)}</span> {a.kind.replace(/_/g, ' ')} <span className="font-mono">{a.node}</span> <span className="text-zinc-500">{a.reason}</span></li>
                ))}
              </ul>
            </>
          )}
        </>
      )}
    </>
  );
}

export default function Status() {
  return (
    <main className="mx-auto max-w-3xl px-4 py-12 sm:py-16">
      <Link href="/" className="text-sm text-zinc-500 hover:underline">← Baton</Link>
      <h1 className="mt-2 text-3xl font-semibold tracking-tight">Status</h1>
      <Suspense fallback={<p className="mt-3 text-zinc-500">Checking…</p>}>
        <Body />
      </Suspense>
      <p className="mt-12 text-sm text-zinc-500">
        These numbers come from the system itself: the event log of every session. Measured performance is on <Link href="/benchmarks" className="underline">benchmarks</Link>.
      </p>
    </main>
  );
}
