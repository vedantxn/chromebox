'use client';
import { useEffect, useRef, useState } from 'react';
import { moves, normalReplyMs } from './data';

// The hero's working panel: a session answering its agent's requests on one
// machine; "Drain" hands it to the other machine mid-session. Each drain
// replays one of our twenty measured migrations (the pause, and the longest
// any reply took during it), in order.

const methods = ['Runtime.evaluate', 'DOM.querySelector', 'Input.dispatchMouseEvent', 'Page.captureScreenshot', 'Runtime.evaluate', 'Network.getCookies'];
type Line = { n: number; method: string; ms: number; slow?: boolean };

export default function Handoff() {
  const [node, setNode] = useState<'a' | 'b'>('a');
  const [moving, setMoving] = useState(false);
  const [lines, setLines] = useState<Line[]>([
    { n: 41, method: 'Runtime.evaluate', ms: 1.8 },
    { n: 40, method: 'Page.captureScreenshot', ms: 2.1 },
    { n: 39, method: 'DOM.querySelector', ms: 1.6 },
    { n: 38, method: 'Input.dispatchMouseEvent', ms: 1.7 },
    { n: 37, method: 'Runtime.evaluate', ms: 1.9 },
  ]);
  const [done, setDone] = useState<{ from: string; to: string; paused: number; wait: number } | null>(null);
  const sample = useRef(0);
  const counter = useRef(42);
  const holding = useRef(false);

  useEffect(() => {
    if (matchMedia('(prefers-reduced-motion: reduce)').matches) return;
    const id = setInterval(() => {
      if (holding.current) return;
      const n = counter.current++;
      const jitter = (Math.sin(n * 1.7) + 1) * 0.35;
      setLines((l) => [{ n, method: methods[n % methods.length], ms: +(normalReplyMs + jitter - 0.2).toFixed(1) }, ...l].slice(0, 7));
    }, 650);
    return () => clearInterval(id);
  }, []);

  const drain = () => {
    if (moving) return;
    const m = moves[sample.current % moves.length];
    sample.current++;
    const from = node === 'a' ? 'node-a' : 'node-b';
    const to = node === 'a' ? 'node-b' : 'node-a';
    setMoving(true);
    setDone(null);
    holding.current = true; // the next reply waits for the move
    setTimeout(() => setNode((x) => (x === 'a' ? 'b' : 'a')), 120);
    setTimeout(() => {
      const n = counter.current++;
      setLines((l) => [{ n, method: 'Runtime.evaluate', ms: m.wait, slow: true }, ...l].slice(0, 7));
      holding.current = false;
      setMoving(false);
      setDone({ from, to, paused: m.paused, wait: m.wait });
    }, Math.max(700, m.wait));
  };

  const other = node === 'a' ? 'node-b' : 'node-a';
  return (
    <div className="handoff" data-ui>
      <div className="handoff__head">
        <span className="handoff__id">vm-7f3a21</span>
        <span className="handoff__state">{moving ? 'migrating' : 'running'}</span>
        <span className="handoff__where">us-east-1</span>
      </div>
      <div className="handoff__lanes" aria-hidden="true">
        {(['a', 'b'] as const).map((k) => (
          <div key={k} className="handoff__lane">
            <span className="handoff__node">node-{k}</span>
            <span className="handoff__track">{node === k && <span className={`handoff__baton${moving ? ' is-moving' : ''}`}>Chrome · 1 tab</span>}</span>
          </div>
        ))}
      </div>
      <div className="handoff__bar">
        <button type="button" className="handoff__drain" onClick={drain} disabled={moving}>
          {moving ? 'Moving…' : `Drain ${node === 'a' ? 'node-a' : 'node-b'}`}
        </button>
        <p className="handoff__result" aria-live="polite">
          {done
            ? `Moved ${done.from} → ${done.to}. Browser paused ${done.paused} ms; the slowest reply took ${done.wait} ms. Requests dropped: 0.`
            : `Drains the machine: the browser moves to ${other} with the agent still connected.`}
        </p>
      </div>
      <table className="handoff__log">
        <caption className="sr-only">Requests from the agent, newest first</caption>
        <thead><tr><th>#</th><th>CDP request</th><th>reply</th></tr></thead>
        <tbody>
          {lines.map((l) => (
            <tr key={l.n} className={l.slow ? 'is-slow' : undefined}>
              <td>{l.n}</td>
              <td>{l.method}</td>
              <td>{l.slow ? `${l.ms} ms, during the move` : `${l.ms} ms`}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <p className="handoff__note">Each drain replays one of {moves.length} measured moves, in order.</p>
    </div>
  );
}
