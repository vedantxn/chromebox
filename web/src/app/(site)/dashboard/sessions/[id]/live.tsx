'use client';
import { useCallback, useEffect, useRef, useState } from 'react';
import { Button } from '@/components/site/button';
import { Panel } from '@/components/site/page';
import { StatusDot } from '../../ui';

type Sess = { id: string; status: string; node?: string; expiresAt?: string; migrations: number; endReason?: string };
type Ev = { at: string; kind: string; detail?: Record<string, number | string | boolean> };

const n0 = (v: unknown) => (typeof v === 'number' ? v.toFixed(0) : '?');

// One line (or two) per thing that happened to the session.
function describe(e: Ev): { title: string; sub?: string; bad?: boolean } {
  const d = e.detail ?? {};
  switch (e.kind) {
    case 'created': return { title: `started on ${d.node}`, sub: `${n0(d.create_ms)} ms from request to a browser that answers` };
    case 'forked_from': return { title: `copy of ${d.parent}` };
    case 'forked': return { title: `copied into ${d.copies} session${d.copies === 1 ? '' : 's'}`, sub: `this browser paused ${n0(d.paused_ms)} ms` };
    case 'migrated': return { title: `${d.from} → ${d.to}`, sub: `paused ${n0(d.paused_ms)} ms · unreachable ${n0(d.unreachable_ms)} ms` +
      (d.stream_ms ? ` · ${n0(d.stream_mib)} MiB of memory followed in ${n0(d.stream_ms)} ms while it ran` : '') };
    case 'migration_failed': return { title: 'migration failed', sub: String(d.error), bad: true };
    case 'paused': return { title: 'paused', sub: d.stored_mib ? `${n0(d.stored_mib)} MiB stored · browser stopped in ${n0(d.paused_ms)} ms` : undefined };
    case 'resumed': return { title: `resumed on ${d.node}`, sub: d.resume_ms ? `${n0(d.resume_ms)} ms` : undefined };
    case 'profile_saved': return { title: `saved as profile “${d.name}”`, sub: `browser paused ${n0(d.paused_ms)} ms · ${n0(d.stored_mib)} MiB stored` };
    case 'started_from_profile': return { title: `started from profile “${d.profile}”`, sub: d.fetched ? `fetched to ${d.image_node} in ${n0(d.image_download_ms)} ms` : 'profile already on this machine' };
    case 'ended': return { title: `ended: ${d.reason}` };
    default: return { title: e.kind };
  }
}

// Live view: a CDP connection from this page, through the gateway, to the
// browser. It attaches to the active tab and asks Chrome to stream it
// (Page.startScreencast); each JPEG frame is drawn and acknowledged. The same
// connection survives a migration, so you see the frames pause and resume.
export default function Live({ id }: { id: string }) {
  const [sess, setSess] = useState<Sess | null>(null);
  const [frame, setFrame] = useState<string | null>(null);
  const [fps, setFps] = useState(0);
  const [lastFrameAgo, setLastFrameAgo] = useState(0);
  const [status, setStatus] = useState('connecting');
  const [events, setEvents] = useState<Ev[]>([]);
  const [migrateError, setMigrateError] = useState('');
  const [profileName, setProfileName] = useState('');
  const [profileMsg, setProfileMsg] = useState('');
  const [sleeping, setSleeping] = useState<'pausing' | 'resuming' | null>(null);
  const [epoch, setEpoch] = useState(0); // bumped when the browser comes back, to reconnect the live view
  const [busy, setBusy] = useState(false);
  const [fork, setFork] = useState<{ pausedMs?: number; sessions?: { id: string }[]; error?: string } | null>(null);
  const [forking, setForking] = useState(false);
  const [url, setUrl] = useState('https://example.com');
  const ws = useRef<WebSocket | null>(null);
  const cdpId = useRef(1);
  const pageSession = useRef<string | null>(null);
  const frames = useRef(0);
  const lastFrame = useRef(Date.now());

  const send = useCallback((method: string, params: object = {}, sessionId?: string) => {
    ws.current?.send(JSON.stringify({ id: cdpId.current++, method, params, ...(sessionId ? { sessionId } : {}) }));
  }, []);

  // Session info and its event log, every 2 s.
  useEffect(() => {
    const load = () => {
      fetch(`/api/v1/sessions/${id}`).then((r) => r.json()).then(setSess).catch(() => {});
      fetch(`/api/v1/sessions/${id}/events`).then((r) => (r.ok ? r.json() : [])).then(setEvents).catch(() => {});
    };
    load();
    const t = setInterval(load, 2000);
    return () => clearInterval(t);
  }, [id]);

  // Frame-rate meter, and how long since the last frame (a migration shows up here).
  useEffect(() => {
    const t = setInterval(() => {
      setFps(frames.current);
      frames.current = 0;
      setLastFrameAgo(Date.now() - lastFrame.current);
    }, 1000);
    return () => clearInterval(t);
  }, []);

  // The CDP connection.
  useEffect(() => {
    let closed = false;
    (async () => {
      const r = await fetch(`/api/v1/sessions/${id}/live`, { method: 'POST' });
      if (!r.ok) { setStatus('not running'); return; }
      const { url: wsUrl } = await r.json();
      const sock = new WebSocket(wsUrl);
      ws.current = sock;
      const pending = new Map<number, (v: any) => void>();
      const call = (method: string, params: object = {}, sessionId?: string) =>
        new Promise<any>((resolve) => {
          const n = cdpId.current++;
          pending.set(n, resolve);
          sock.send(JSON.stringify({ id: n, method, params, ...(sessionId ? { sessionId } : {}) }));
        });
      sock.onmessage = (ev) => {
        const m = JSON.parse(ev.data);
        if (m.id && pending.has(m.id)) { pending.get(m.id)!(m.result); pending.delete(m.id); return; }
        if (m.method === 'Page.screencastFrame') {
          setFrame(`data:image/jpeg;base64,${m.params.data}`);
          frames.current++;
          lastFrame.current = Date.now();
          send('Page.screencastFrameAck', { sessionId: m.params.sessionId }, m.sessionId);
        }
      };
      sock.onclose = () => { if (!closed) setStatus('disconnected'); };
      sock.onopen = async () => {
        setStatus('connected');
        const { targetInfos } = await call('Target.getTargets');
        let page = targetInfos.find((t: any) => t.type === 'page');
        if (!page) page = { targetId: (await call('Target.createTarget', { url: 'about:blank' })).targetId };
        const { sessionId } = await call('Target.attachToTarget', { targetId: page.targetId, flatten: true });
        pageSession.current = sessionId;
        await call('Page.enable', {}, sessionId);
        await call('Page.startScreencast', { format: 'jpeg', quality: 60, maxWidth: 1280, maxHeight: 800 }, sessionId);
      };
    })();
    return () => { closed = true; ws.current?.close(); };
  }, [id, send, epoch]);

  // Fork: a copy of this browser, its pages, cookies and storage, in a new session.
  const forkOne = async () => {
    setForking(true);
    const r = await fetch(`/api/v1/sessions/${id}/fork`, { method: 'POST', body: JSON.stringify({ count: 1 }) });
    setFork(await r.json());
    setForking(false);
  };
  const migrate = async () => {
    setBusy(true);
    const r = await fetch(`/api/v1/sessions/${id}/migrate`, { method: 'POST' });
    const d = await r.json();
    setMigrateError(r.ok ? '' : String(d.error ?? 'migration failed'));
    setBusy(false);
  };
  // Save this browser's pages, cookies and storage as a profile; it keeps running.
  const saveProfile = async () => {
    setProfileMsg('saving…');
    const r = await fetch('/api/v1/profiles', { method: 'POST', body: JSON.stringify({ session: id, name: profileName }) });
    const d = await r.json();
    setProfileMsg(r.ok ? `saved “${d.name}” (${d.storedMiB.toFixed(0)} MiB; this browser paused ${d.pausedMs.toFixed(0)} ms)` : String(d.error ?? 'could not save'));
    if (r.ok) setProfileName('');
  };
  // Pause stores the browser and frees its machine; resume brings it back on any machine.
  const pause = async () => {
    setSleeping('pausing');
    setMigrateError('');
    const r = await fetch(`/api/v1/sessions/${id}/pause`, { method: 'POST' });
    if (!r.ok) setMigrateError(String((await r.json()).error ?? 'pause failed'));
    setFrame(null);
    setSleeping(null);
  };
  const resume = async () => {
    setSleeping('resuming');
    setMigrateError('');
    const r = await fetch(`/api/v1/sessions/${id}/resume`, { method: 'POST' });
    if (!r.ok) setMigrateError(String((await r.json()).error ?? 'resume failed'));
    else setEpoch((e) => e + 1);
    setSleeping(null);
  };
  const navigate = () => pageSession.current && send('Page.navigate', { url }, pageSession.current);
  const end = async () => { await fetch(`/api/v1/sessions/${id}`, { method: 'DELETE' }); location.href = '/dashboard'; };

  const running = sess?.status === 'running';
  const paused = sess?.status === 'paused';
  const input = 'h-8 min-w-0 rounded-sm bg-muted px-3 font-mono text-[12.5px] text-foreground outline-none transition-shadow placeholder:text-subtle focus:shadow-[0_0_0_1.5px_var(--brand)]';
  return (
    <div className="mt-8 grid gap-5 lg:grid-cols-12">
      <Panel className="min-w-0 lg:col-span-8">
        <div className="flex items-center gap-3 border-b border-rule px-3 py-2 sm:px-4">
          <span aria-hidden="true" className="hidden gap-1.5 sm:flex">
            <span className="size-2.5 rounded-full bg-rule-strong" />
            <span className="size-2.5 rounded-full bg-rule-strong" />
            <span className="size-2.5 rounded-full bg-rule-strong" />
          </span>
          <form className="flex min-w-0 flex-1 gap-2" onSubmit={(e) => { e.preventDefault(); navigate(); }}>
            <input aria-label="Address" value={url} onChange={(e) => setUrl(e.target.value)} className={`${input} flex-1`} />
            <Button size="sm" variant="secondary" type="submit">Go</Button>
          </form>
          <span className="hidden items-center gap-1.5 font-mono text-[11px] text-subtle sm:flex">
            <StatusDot status={sess?.status} />
            {paused ? 'PAUSED' : status === 'connected' ? 'LIVE' : status.toUpperCase()}
            {sess?.node ? ` · ${sess.node}` : ''}
          </span>
        </div>
        <div className="aspect-[16/10] bg-stage">
          {frame ? <img src={frame} alt="live view of the browser" className="h-full w-full object-contain" />
            : <div className="flex h-full items-center justify-center px-6 text-center text-[14px] text-subtle">
                {paused ? 'Paused: stored for later, using no machine. Resume to bring it back exactly as it was.'
                  : status === 'connected' ? 'Waiting for the first frame…' : status}
              </div>}
        </div>
        <p className="tabular flex flex-wrap gap-x-3 border-t border-rule px-4 py-2 font-mono text-[11.5px] text-subtle">
          <span>{status}</span><span>{fps} fps</span><span>last frame {(lastFrameAgo / 1000).toFixed(1)} s ago</span>
        </p>
      </Panel>

      <aside className="min-w-0 space-y-5 lg:col-span-4">
        <Panel>
          <div className="px-5 py-4">
            <p className="label-mono text-subtle">Status</p>
            <p className="mt-1.5 flex items-center gap-2 text-[16px] font-medium">
              <StatusDot status={sess?.status} />
              {sess?.status ?? '…'}{sess?.node ? ` on ${sess.node}` : ''}
            </p>
            {sess?.expiresAt && <p className="mt-0.5 text-[13px] text-subtle">ends {new Date(sess.expiresAt).toLocaleTimeString()}</p>}
          </div>
          <div className="flex flex-wrap gap-2 border-t border-rule px-5 py-4">
            {paused || sleeping === 'resuming' ? (
              <Button size="sm" variant="primary" disabled={sleeping === 'resuming'} onClick={resume}>
                {sleeping === 'resuming' ? 'Resuming…' : 'Resume'}
              </Button>
            ) : (<>
              <Button size="sm" variant="primary" disabled={busy || !running} onClick={migrate}>
                {busy ? 'Moving…' : 'Migrate now'}
              </Button>
              <Button size="sm" variant="secondary" disabled={forking || !running} onClick={forkOne}>
                {forking ? 'Forking…' : 'Fork'}
              </Button>
              <Button size="sm" variant="secondary" disabled={sleeping === 'pausing' || !running} onClick={pause}>
                {sleeping === 'pausing' ? 'Pausing…' : 'Pause'}
              </Button>
            </>)}
            <Button size="sm" variant="ghost" onClick={end}>End</Button>
          </div>
          {migrateError && <p className="border-t border-rule px-5 py-3 text-[13px] text-brand-ink">{migrateError}</p>}
          {fork && (
            <p className="border-t border-rule px-5 py-3 text-[13px] text-muted-foreground">
              {fork.error ? <span className="text-brand-ink">{fork.error}</span> : <>
                Forked; this browser paused {fork.pausedMs?.toFixed(0)} ms.{' '}
                {fork.sessions?.map((s) => <a key={s.id} href={`/dashboard/sessions/${s.id}`} className="font-mono text-foreground underline underline-offset-[3px]">{s.id}</a>)}
                {' '}has the same pages, cookies and storage.
              </>}
            </p>
          )}
          {running && (
            <div className="border-t border-rule px-5 py-4">
              <form className="flex gap-2" onSubmit={(e) => { e.preventDefault(); if (profileName) saveProfile(); }}>
                <input aria-label="Profile name" value={profileName} onChange={(e) => setProfileName(e.target.value.toLowerCase())} placeholder="profile name"
                  className={`${input} flex-1`} />
                <Button size="sm" variant="secondary" type="submit" disabled={!profileName}>Save profile</Button>
              </form>
              {profileMsg && <p className="mt-2 text-[13px] text-subtle">{profileMsg}</p>}
            </div>
          )}
        </Panel>

        <Panel>
          <div className="border-b border-rule px-5 py-3">
            <h2 className="text-[15px] font-medium tracking-[-0.01em]">Events</h2>
          </div>
          <ol className="px-5 py-2">
            {events.length === 0 && <li className="py-2 text-[14px] text-subtle">Nothing yet.</li>}
            {[...events].reverse().map((e, i) => {
              const x = describe(e);
              return (
                <li key={i} className="relative border-l border-rule py-2 pl-4 text-[13.5px]">
                  <span aria-hidden="true" className={`absolute top-[14px] -left-[3.5px] size-1.5 rounded-full ${x.bad ? 'bg-brand' : i === 0 ? 'bg-foreground' : 'bg-ink-400'}`} />
                  <span className="tabular font-mono text-[11.5px] text-subtle">{new Date(e.at).toLocaleTimeString()}</span>
                  <p className={x.bad ? 'text-brand-ink' : 'text-foreground'}>{x.title}</p>
                  {x.sub && <p className="text-[12.5px] text-muted-foreground">{x.sub}</p>}
                </li>
              );
            })}
          </ol>
        </Panel>
      </aside>
    </div>
  );
}
