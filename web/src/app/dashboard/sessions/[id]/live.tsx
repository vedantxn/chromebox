'use client';
import { useCallback, useEffect, useRef, useState } from 'react';

type Sess = { id: string; status: string; node?: string; expiresAt?: string; migrations: number; endReason?: string };
type Move = { at: string; from?: string; to?: string; pausedMs?: number; unreachableMs?: number; streamMs?: number; streamMiB?: number; error?: string };

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
  const [moves, setMoves] = useState<Move[]>([]);
  const [busy, setBusy] = useState(false);
  const [url, setUrl] = useState('https://example.com');
  const ws = useRef<WebSocket | null>(null);
  const cdpId = useRef(1);
  const pageSession = useRef<string | null>(null);
  const frames = useRef(0);
  const lastFrame = useRef(Date.now());

  const send = useCallback((method: string, params: object = {}, sessionId?: string) => {
    ws.current?.send(JSON.stringify({ id: cdpId.current++, method, params, ...(sessionId ? { sessionId } : {}) }));
  }, []);

  // Session info, every 2 s.
  useEffect(() => {
    const load = () => fetch(`/api/v1/sessions/${id}`).then((r) => r.json()).then(setSess).catch(() => {});
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
  }, [id, send]);

  const migrate = async () => {
    setBusy(true);
    const r = await fetch(`/api/v1/sessions/${id}/migrate`, { method: 'POST' });
    const d = await r.json();
    setMoves((m) => [{ at: new Date().toLocaleTimeString(), ...d }, ...m]);
    setBusy(false);
  };
  const navigate = () => pageSession.current && send('Page.navigate', { url }, pageSession.current);
  const end = async () => { await fetch(`/api/v1/sessions/${id}`, { method: 'DELETE' }); location.href = '/dashboard'; };

  return (
    <div className="mt-4 grid gap-6 lg:grid-cols-[1fr_280px]">
      <div>
        <div className="flex gap-2">
          <input value={url} onChange={(e) => setUrl(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && navigate()}
            className="min-w-0 flex-1 rounded-md border border-zinc-300 bg-transparent px-3 py-1.5 text-sm dark:border-zinc-700" />
          <button onClick={navigate} className="rounded-md border border-zinc-300 px-3 py-1.5 text-sm dark:border-zinc-700">Go</button>
        </div>
        <div className="mt-3 aspect-[16/10] overflow-hidden rounded-lg border border-zinc-200 bg-zinc-100 dark:border-zinc-800 dark:bg-zinc-900">
          {frame ? <img src={frame} alt="live view of the browser" className="h-full w-full object-contain" />
            : <div className="flex h-full items-center justify-center text-sm text-zinc-500">{status === 'connected' ? 'waiting for the first frame…' : status}</div>}
        </div>
        <p className="mt-2 text-xs text-zinc-500">
          {status} · {fps} fps · last frame {(lastFrameAgo / 1000).toFixed(1)} s ago
        </p>
      </div>
      <aside className="space-y-5 text-sm">
        <div>
          <p className="text-zinc-500">Status</p>
          <p className="font-medium">{sess?.status ?? '…'}{sess?.node ? ` on ${sess.node}` : ''}</p>
          {sess?.expiresAt && <p className="text-xs text-zinc-500">ends {new Date(sess.expiresAt).toLocaleTimeString()}</p>}
        </div>
        <div className="flex gap-2">
          <button disabled={busy || sess?.status !== 'running'} onClick={migrate}
            className="rounded-md bg-zinc-900 px-3 py-1.5 text-white disabled:opacity-40 dark:bg-white dark:text-zinc-900">
            {busy ? 'Moving…' : 'Migrate now'}
          </button>
          <button onClick={end} className="rounded-md border border-zinc-300 px-3 py-1.5 dark:border-zinc-700">End</button>
        </div>
        <div>
          <p className="text-zinc-500">Migrations</p>
          <ul className="mt-1 space-y-2">
            {moves.length === 0 && <li className="text-zinc-500">None yet. Press “Migrate now”: the browser resumes on the other machine before its memory arrives, and the memory follows while it runs.</li>}
            {moves.map((m, i) => (
              <li key={i} className="rounded-md border border-zinc-200 p-2 dark:border-zinc-800">
                <span className="text-zinc-500">{m.at}</span>{' '}
                {m.error ? <span className="text-red-600">{m.error}</span>
                  : <>{m.from} → {m.to}<br /><span className="text-zinc-500">paused {m.pausedMs?.toFixed(0)} ms · unreachable {m.unreachableMs?.toFixed(0)} ms
                    {m.streamMs ? <><br />then {m.streamMiB?.toFixed(0)} MiB of memory followed in {m.streamMs.toFixed(0)} ms while it ran</> : null}</span></>}
              </li>
            ))}
          </ul>
        </div>
      </aside>
    </div>
  );
}
