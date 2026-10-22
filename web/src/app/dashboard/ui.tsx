'use client';
import Link from 'next/link';
import { useCallback, useEffect, useState } from 'react';

type Key = { id: string; name: string; prefix: string; created_at: string; revoked: boolean };
type Sess = { id: string; status: string; node?: string; createdAt: string; endedAt?: string; endReason?: string; migrations: number };
type Usage = { running: number; max_running: number; minutes_today: number; max_minutes_per_day: number };

const j = (r: Response) => (r.ok ? r.json() : r.json().then((e) => Promise.reject(new Error(e.error ?? r.statusText))));

export default function Dashboard() {
  const [keys, setKeys] = useState<Key[]>([]);
  const [sessions, setSessions] = useState<Sess[]>([]);
  const [usage, setUsage] = useState<Usage | null>(null);
  const [newKey, setNewKey] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const [k, s, u] = await Promise.all([fetch('/api/v1/keys').then(j), fetch('/api/v1/sessions').then(j), fetch('/api/v1/usage').then(j)]);
      setKeys(k); setSessions(s); setUsage(u);
    } catch (e) { setErr(String(e)); }
  }, []);
  useEffect(() => { load(); const t = setInterval(load, 3000); return () => clearInterval(t); }, [load]);

  const createKey = async () => {
    const k = await fetch('/api/v1/keys', { method: 'POST', body: JSON.stringify({ name: 'key ' + (keys.length + 1) }) }).then(j);
    setNewKey(k.key); load();
  };
  const startSession = async () => {
    setErr(null);
    try { await fetch('/api/v1/sessions', { method: 'POST' }).then(j); } catch (e) { setErr(String(e)); }
    load();
  };

  return (
    <div className="mt-8 space-y-10">
      {err && <p className="rounded-md bg-red-50 p-3 text-sm text-red-700 dark:bg-red-950 dark:text-red-300">{err}</p>}

      <section>
        <h2 className="font-medium">API keys</h2>
        {newKey && (
          <div className="mt-3 rounded-md border border-amber-300 bg-amber-50 p-3 text-sm dark:border-amber-700 dark:bg-amber-950">
            Copy this key now; it won&apos;t be shown again:
            <code className="mt-2 block break-all font-mono">{newKey}</code>
          </div>
        )}
        <ul className="mt-3 divide-y divide-zinc-200 text-sm dark:divide-zinc-800">
          {keys.map((k) => (
            <li key={k.id} className="flex items-center justify-between py-2">
              <span><code className="font-mono">{k.prefix}…</code> <span className="text-zinc-500">{k.name}</span></span>
              {k.revoked ? <span className="text-zinc-400">revoked</span> : (
                <button className="text-red-600 hover:underline" onClick={async () => { await fetch(`/api/v1/keys/${k.id}`, { method: 'DELETE' }); load(); }}>Revoke</button>
              )}
            </li>
          ))}
        </ul>
        <button onClick={createKey} className="mt-3 rounded-md bg-zinc-900 px-3 py-1.5 text-sm text-white dark:bg-white dark:text-zinc-900">Create key</button>
      </section>

      <section>
        <div className="flex items-center justify-between">
          <h2 className="font-medium">Sessions</h2>
          {usage && <span className="text-sm text-zinc-500">{usage.running}/{usage.max_running} running · {usage.minutes_today.toFixed(1)}/{usage.max_minutes_per_day} min today</span>}
        </div>
        <ul className="mt-3 divide-y divide-zinc-200 text-sm dark:divide-zinc-800">
          {sessions.length === 0 && <li className="py-2 text-zinc-500">No sessions yet. Create a key and run the quickstart, or start one here.</li>}
          {sessions.map((s) => (
            <li key={s.id} className="flex flex-wrap items-center justify-between gap-2 py-2">
              <Link href={`/dashboard/sessions/${s.id}`} className="font-mono hover:underline">{s.id}</Link>
              <span className="text-zinc-500">
                {s.status}{s.node ? ` on ${s.node}` : ''}{s.migrations ? ` · ${s.migrations} migration${s.migrations > 1 ? 's' : ''}` : ''}
                {s.endReason ? ` · ${s.endReason}` : ''}
              </span>
            </li>
          ))}
        </ul>
        <button onClick={startSession} className="mt-3 rounded-md border border-zinc-300 px-3 py-1.5 text-sm dark:border-zinc-700">Start a session</button>
      </section>
    </div>
  );
}
