'use client';
import { Plus } from 'lucide-react';
import Link from 'next/link';
import { useCallback, useEffect, useState } from 'react';
import { Button } from '@/components/site/button';
import { CopyButton } from '@/components/site/copy-button';
import { Panel, Stat, StatRow } from '@/components/site/page';
import { cn } from '@/lib/cn';

type Key = { id: string; name: string; prefix: string; created_at: string; revoked: boolean };
type Sess = { id: string; status: string; node?: string; createdAt: string; endedAt?: string; endReason?: string; migrations: number };
type Usage = { running: number; max_running: number; paused?: number; minutes_today: number; max_minutes_per_day: number };
type Profile = { name: string; createdAt: string; storedMiB: number };

const j = (r: Response) => (r.ok ? r.json() : r.json().then((e) => Promise.reject(new Error(e.error ?? r.statusText))));

export default function Dashboard() {
  const [keys, setKeys] = useState<Key[]>([]);
  const [sessions, setSessions] = useState<Sess[]>([]);
  const [usage, setUsage] = useState<Usage | null>(null);
  const [profiles, setProfiles] = useState<Profile[]>([]);
  const [newKey, setNewKey] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const [k, s, u, p] = await Promise.all([fetch('/api/v1/keys').then(j), fetch('/api/v1/sessions').then(j), fetch('/api/v1/usage').then(j), fetch('/api/v1/profiles').then(j)]);
      setKeys(k); setSessions(s); setUsage(u); setProfiles(p);
    } catch (e) { setErr(String(e)); }
  }, []);
  useEffect(() => { load(); const t = setInterval(load, 3000); return () => clearInterval(t); }, [load]);

  const createKey = async () => {
    const k = await fetch('/api/v1/keys', { method: 'POST', body: JSON.stringify({ name: 'key ' + (keys.length + 1) }) }).then(j);
    setNewKey(k.key); load();
  };
  const startFromProfile = async (name: string) => {
    setErr(null);
    try {
      const s = await fetch('/api/v1/sessions', { method: 'POST', body: JSON.stringify({ profile: name }) }).then(j);
      location.href = `/dashboard/sessions/${s.id}`;
    } catch (e) { setErr(String(e)); }
  };
  const startSession = async () => {
    setErr(null);
    try { await fetch('/api/v1/sessions', { method: 'POST' }).then(j); } catch (e) { setErr(String(e)); }
    load();
  };

  const live = keys.filter((k) => !k.revoked);
  return (
    <div className="mt-10 space-y-5">
      {err ? (
        <p role="alert" className="rounded-md bg-brand-tint px-4 py-3 text-[14px] text-brand-ink">
          {err}
        </p>
      ) : null}

      {usage ? (
        <StatRow cols={3}>
          <Stat value={<>{usage.running}<span className="text-subtle"> / {usage.max_running}</span></>} label="sessions running" />
          <Stat value={usage.paused ?? 0} label="paused, using no machine" />
          <Stat
            value={<>{usage.minutes_today.toFixed(1)}<span className="text-subtle"> / {usage.max_minutes_per_day}</span></>}
            label="browser minutes today"
          />
        </StatRow>
      ) : null}

      <div className="grid gap-5 lg:grid-cols-12">
        <Panel className="lg:col-span-7">
          <div className="flex flex-wrap items-center justify-between gap-3 border-b border-rule px-5 py-3 sm:px-6">
            <h2 className="text-[15px] font-medium tracking-[-0.01em]">Sessions</h2>
            <Button size="sm" variant="secondary" onClick={startSession}>
              <Plus className="size-3.5" strokeWidth={2} aria-hidden="true" />
              Start a session
            </Button>
          </div>
          <ul>
            {sessions.length === 0 ? (
              <li className="px-5 py-4 text-[14px] text-subtle sm:px-6">
                No sessions yet. Create a key and run the <Link href="/docs" className="text-foreground underline underline-offset-[3px]">quickstart</Link>, or start one here.
              </li>
            ) : null}
            {sessions.map((s) => (
              <li key={s.id} className="border-t border-rule first:border-t-0">
                <Link
                  href={`/dashboard/sessions/${s.id}`}
                  className="group flex flex-wrap items-center justify-between gap-x-4 gap-y-1 px-5 py-3 transition-colors hover:bg-muted/60 sm:px-6"
                >
                  <span className="flex items-center gap-2.5 font-mono text-[13px]">
                    <StatusDot status={s.status} />
                    <span className="link-draw link-draw-quiet">{s.id}</span>
                  </span>
                  <span className="text-[13px] text-muted-foreground">
                    {s.status}
                    {s.node ? ` on ${s.node}` : ""}
                    {s.migrations ? ` · ${s.migrations} migration${s.migrations > 1 ? "s" : ""}` : ""}
                    {s.endReason ? ` · ${s.endReason}` : ""}
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        </Panel>

        <div className="space-y-5 lg:col-span-5">
          <Panel>
            <div className="flex items-center justify-between gap-3 border-b border-rule px-5 py-3 sm:px-6">
              <h2 className="text-[15px] font-medium tracking-[-0.01em]">API keys</h2>
              <Button size="sm" variant="primary" onClick={createKey}>
                <Plus className="size-3.5" strokeWidth={2} aria-hidden="true" />
                Create key
              </Button>
            </div>
            {newKey ? (
              <div className="border-b border-rule bg-brand-tint/60 px-5 py-4 sm:px-6">
                <p className="text-[13px] text-brand-ink">Copy this key now; it won’t be shown again.</p>
                <div className="mt-2 flex items-start gap-2 rounded-md bg-card px-3 py-2.5 shadow-sm">
                  <code className="min-w-0 flex-1 font-mono text-[12px] leading-[1.6] break-all">{newKey}</code>
                  <CopyButton value={newKey} className="-my-1" />
                </div>
              </div>
            ) : null}
            <ul>
              {keys.length === 0 ? (
                <li className="px-5 py-4 text-[14px] text-subtle sm:px-6">No keys yet.</li>
              ) : null}
              {keys.map((k) => (
                <li
                  key={k.id}
                  className="flex items-center justify-between gap-3 border-t border-rule px-5 py-2.5 text-[14px] first:border-t-0 sm:px-6"
                >
                  <span className={cn("min-w-0 truncate", k.revoked && "text-subtle line-through")}>
                    <code className="font-mono text-[13px]">{k.prefix}…</code>{" "}
                    <span className="text-muted-foreground">{k.name}</span>
                  </span>
                  {k.revoked ? (
                    <span className="text-[13px] text-subtle">revoked</span>
                  ) : (
                    <button
                      type="button"
                      className="text-[13px] text-muted-foreground transition-colors hover:text-brand-ink"
                      onClick={async () => {
                        await fetch(`/api/v1/keys/${k.id}`, { method: "DELETE" });
                        setNewKey(null);
                        load();
                      }}
                    >
                      Revoke
                    </button>
                  )}
                </li>
              ))}
            </ul>
            {live.length > 0 ? (
              <p className="border-t border-rule px-5 py-3 text-[13px] text-subtle sm:px-6">
                Set it as <code className="font-mono text-[12px] text-foreground">BATON_API_KEY</code>; see the <Link href="/docs" className="text-foreground underline underline-offset-[3px]">quickstart</Link>.
              </p>
            ) : null}
          </Panel>

          <Panel>
            <div className="border-b border-rule px-5 py-3 sm:px-6">
              <h2 className="text-[15px] font-medium tracking-[-0.01em]">Profiles</h2>
            </div>
            <ul>
              {profiles.length === 0 ? (
                <li className="px-5 py-4 text-[14px] text-subtle sm:px-6">
                  None yet. Open a session, log in, and save it as a profile; sessions started from it
                  open already logged in.
                </li>
              ) : null}
              {profiles.map((p) => (
                <li
                  key={p.name}
                  className="flex items-center justify-between gap-3 border-t border-rule px-5 py-2.5 text-[14px] first:border-t-0 sm:px-6"
                >
                  <span className="min-w-0 truncate">
                    <code className="font-mono text-[13px]">{p.name}</code>{" "}
                    <span className="text-[13px] text-subtle">
                      {Math.round(p.storedMiB)} MiB · {new Date(p.createdAt).toLocaleDateString()}
                    </span>
                  </span>
                  <span className="flex shrink-0 gap-3 text-[13px]">
                    <button type="button" className="font-medium text-foreground hover:text-brand-ink" onClick={() => startFromProfile(p.name)}>
                      Start
                    </button>
                    <button
                      type="button"
                      className="text-muted-foreground hover:text-brand-ink"
                      onClick={async () => {
                        await fetch(`/api/v1/profiles/${p.name}`, { method: "DELETE" });
                        load();
                      }}
                    >
                      Delete
                    </button>
                  </span>
                </li>
              ))}
            </ul>
          </Panel>
        </div>
      </div>
    </div>
  );
}

/** Green while it runs, rust while it moves, grey once it has ended. */
export function StatusDot({ status }: { status?: string }) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        "inline-block size-2 shrink-0 rounded-full",
        status === "running" ? "bg-success" : status === "migrating" || status === "creating" ? "bg-brand" : status === "paused" ? "bg-warning" : "bg-ink-400",
      )}
    />
  );
}
