// A session past its 15-minute lifetime must end by itself: the VM is deleted
// and the agent's open connection is closed, not left hanging.
// Usage: node expiry-check.mjs <base url> <api key>
import { chromium } from 'playwright-core';
const [base, key] = process.argv.slice(2);
const h = { Authorization: `Bearer ${key}` };
const s = await (await fetch(base + '/api/v1/sessions', { method: 'POST', headers: h })).json();
const browser = await chromium.connectOverCDP(s.connectUrl);
const t0 = Date.now();
console.log(`session ${s.id} expires ${s.expiresAt}; holding the connection idle`);
const closed = new Promise((r) => browser.on('disconnected', r));
await Promise.race([closed, new Promise((r) => setTimeout(r, 20 * 60 * 1000))]);
const late = (Date.now() - Date.parse(s.expiresAt)) / 1000;
console.log(browser.isConnected() ? 'still connected after 20 min (bad)'
  : `connection closed ${((Date.now() - t0) / 60000).toFixed(1)} min after connecting, ${late.toFixed(1)} s after expiry`);
const after = await (await fetch(`${base}/api/v1/sessions/${s.id}`, { headers: h })).json();
console.log(`session now: ${after.status}${after.endReason ? ` (${after.endReason})` : ''}`);
