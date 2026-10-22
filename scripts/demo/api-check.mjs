// Use the public API exactly as a customer would: API key -> session -> Playwright.
import { chromium } from 'playwright-core';
const [base, key] = process.argv.slice(2);
const call = async (method, path) => {
  const r = await fetch(base + path, { method, headers: { Authorization: `Bearer ${key}` } });
  const body = r.status === 204 ? null : await r.json();
  if (!r.ok) throw new Error(`${method} ${path}: ${r.status} ${JSON.stringify(body)}`);
  return body;
};
let t = Date.now();
const s = await call('POST', '/api/v1/sessions');
console.log(`session ${s.id} on ${s.node}, created in ${Date.now() - t} ms, expires ${s.expiresAt}`);
t = Date.now();
const browser = await chromium.connectOverCDP(s.connectUrl);
const page = await browser.contexts()[0].newPage();
await page.goto('https://example.com');
console.log(`connected and loaded a page in ${Date.now() - t} ms: ${await page.title()}`);
const second = await call('POST', '/api/v1/sessions');
try { await call('POST', '/api/v1/sessions'); console.log('third session: ALLOWED (bad)'); }
catch (e) { console.log('third concurrent session refused:', e.message.split(': ').slice(-1)[0]); }
console.log('usage:', JSON.stringify(await call('GET', '/api/v1/usage')));
for (const id of [s.id, second.id]) await call('DELETE', `/api/v1/sessions/${id}`);
try { await page.title(); } catch {}
console.log('sessions:', (await call('GET', '/api/v1/sessions')).map((x) => `${x.id}:${x.status}`).join(' '));
try { await call('GET', '/api/v1/sessions'.replace('sessions', 'usage')); } catch {}
const bad = await fetch(base + '/api/v1/sessions', { headers: { Authorization: 'Bearer bk_wrong' } });
console.log('wrong key:', bad.status);
await browser.close().catch(() => {});
