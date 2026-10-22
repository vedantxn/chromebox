// The dashboard's live view, without the dashboard: open the session's view URL,
// stream the page with Page.startScreencast, migrate in the middle, and report
// the frame rate and the longest gap between frames. An agent keeps its own
// Playwright connection open the whole time; both must survive the move.
// Usage: node live-check.mjs <base url> <api key>
import { chromium } from 'playwright-core';
const [base, key] = process.argv.slice(2);
const call = async (method, path) => {
  const r = await fetch(base + path, { method, headers: { Authorization: `Bearer ${key}` } });
  const body = r.status === 204 ? null : await r.json();
  if (!r.ok) throw new Error(`${method} ${path}: ${r.status} ${JSON.stringify(body)}`);
  return body;
};

const s = await call('POST', '/api/v1/sessions');
console.log(`session ${s.id} on ${s.node}`);
const browser = await chromium.connectOverCDP(s.connectUrl);
const page = browser.contexts()[0].pages()[0] ?? (await browser.contexts()[0].newPage());
// A page that changes every frame, so Chrome always has a new frame to send.
await page.goto('data:text/html,<body style="font:64px monospace"><div id=t></div><script>' +
  'const f=()=>{t.textContent=performance.now().toFixed(0);requestAnimationFrame(f)};f()</script>');

const { url } = await call('POST', `/api/v1/sessions/${s.id}/live`);
const ws = new WebSocket(url);
let n = 1;
const pending = new Map();
const cdp = (method, params = {}, sessionId) => new Promise((resolve) => {
  const id = n++;
  pending.set(id, resolve);
  ws.send(JSON.stringify({ id, method, params, ...(sessionId ? { sessionId } : {}) }));
});
const frames = [];
ws.onmessage = (ev) => {
  const m = JSON.parse(ev.data);
  if (m.id && pending.has(m.id)) { pending.get(m.id)(m.result); pending.delete(m.id); return; }
  if (m.method === 'Page.screencastFrame') {
    frames.push(Date.now());
    ws.send(JSON.stringify({ id: n++, method: 'Page.screencastFrameAck', params: { sessionId: m.params.sessionId }, sessionId: m.sessionId }));
  }
};
let closed = false;
ws.onclose = () => { closed = true; };
await new Promise((r) => (ws.onopen = r));
const { targetInfos } = await cdp('Target.getTargets');
const target = targetInfos.find((t) => t.type === 'page' && t.url.startsWith('data:'));
const { sessionId } = await cdp('Target.attachToTarget', { targetId: target.targetId, flatten: true });
await cdp('Page.enable', {}, sessionId);
const t0 = Date.now();
await cdp('Page.startScreencast', { format: 'jpeg', quality: 60, maxWidth: 1280, maxHeight: 800 }, sessionId);
console.log(`first frame after ${await new Promise((r) => { const i = setInterval(() => frames.length && (clearInterval(i), r(frames[0] - t0)), 5); })} ms`);

const fps = (from, to) => frames.filter((f) => f >= from && f < to).length / ((to - from) / 1000);
const maxGap = (from, to) => {
  const fs = frames.filter((f) => f >= from && f <= to);
  return Math.max(...fs.slice(1).map((f, i) => f - fs[i]));
};
await new Promise((r) => setTimeout(r, 5000));
const before = Date.now();
console.log(`before: ${fps(before - 5000, before).toFixed(1)} fps, longest gap ${maxGap(before - 5000, before)} ms`);

const mv = await call('POST', `/api/v1/sessions/${s.id}/migrate`);
console.log(`migrated ${mv.from} -> ${mv.to}: paused ${mv.pausedMs.toFixed(0)} ms, unreachable ${mv.unreachableMs.toFixed(0)} ms`);
await new Promise((r) => setTimeout(r, 5000));
const after = Date.now();
console.log(`across the move: longest gap ${maxGap(before, after)} ms; after: ${fps(after - 3000, after).toFixed(1)} fps`);
console.log(`live view connection ${closed ? 'CLOSED (bad)' : 'still open'}; agent sees ${await page.evaluate(() => t.textContent)} (page clock)`);
console.log(`agent connection ${browser.isConnected() ? 'still open' : 'CLOSED (bad)'}`);
ws.close();
await browser.close().catch(() => {});
await call('DELETE', `/api/v1/sessions/${s.id}`);
