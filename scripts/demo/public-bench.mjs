// Public-path numbers, measured the way a customer meets them (through Vercel
// and CloudFront): create a session, connect Playwright, load a page, end it.
// Spaced out to stay under the 10-creates-a-minute limit; then 11 quick creates
// to show the limit itself.
// Usage: node public-bench.mjs <base url> <api key> [rounds]
import { chromium } from 'playwright-core';
const [base, key, rounds = '16'] = process.argv.slice(2);
const h = { Authorization: `Bearer ${key}` };
const ms = (t) => Date.now() - t;
const pct = (xs, p) => [...xs].sort((a, b) => a - b)[Math.min(xs.length - 1, Math.floor(xs.length * p))];
const create = [], connect = [], load = [], end = [];
for (let i = 0; i < +rounds; i++) {
  const start = Date.now();
  let t = Date.now();
  const r = await fetch(base + '/api/v1/sessions', { method: 'POST', headers: h });
  const s = await r.json();
  if (!r.ok) { console.log('create failed:', r.status, s); break; }
  create.push(ms(t));
  t = Date.now();
  const browser = await chromium.connectOverCDP(s.connectUrl);
  connect.push(ms(t));
  t = Date.now();
  const page = browser.contexts()[0].pages()[0] ?? (await browser.contexts()[0].newPage());
  await page.goto('https://example.com');
  load.push(ms(t));
  t = Date.now();
  await fetch(`${base}/api/v1/sessions/${s.id}`, { method: 'DELETE', headers: h });
  end.push(ms(t));
  await browser.close().catch(() => {});
  await new Promise((r) => setTimeout(r, Math.max(0, 6500 - ms(start))));
}
for (const [name, xs] of [['create', create], ['connect', connect], ['load example.com', load], ['end', end]])
  console.log(`${name.padEnd(17)} p50 ${pct(xs, 0.5)} ms  p95 ${pct(xs, 0.95)} ms  (n=${xs.length})`);

await new Promise((r) => setTimeout(r, 61000));
const codes = [];
for (let i = 0; i < 11; i++) {
  const r = await fetch(base + '/api/v1/sessions', { method: 'POST', headers: h });
  const s = await r.json();
  codes.push(r.ok ? r.status : `${r.status} ${s.error ?? ''}`.trim());
  if (r.ok) await fetch(`${base}/api/v1/sessions/${s.id}`, { method: 'DELETE', headers: h });
}
console.log('11 creates in a row:', codes.join(', '));
