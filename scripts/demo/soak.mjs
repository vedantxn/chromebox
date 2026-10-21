// Keep a CDP connection busy while the VM is migrated, and report what the
// agent saw. A request every 100 ms asks the page for an in-page counter that
// ticks on its own timer; if the connection survives and the page's state is
// intact, the counter never goes backwards and the marker never changes.
// Usage: node soak.mjs <gateway-ip> [vm-index] [seconds]
import { chromium } from 'playwright-core';

const [gateway, index = '1', seconds = '60'] = process.argv.slice(2);
if (!gateway) {
  console.error('usage: node soak.mjs <gateway-ip> [vm-index] [seconds]');
  process.exit(2);
}
const stamp = () => new Date().toISOString().slice(11, 23);
const log = (...a) => console.log(stamp(), ...a);
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const browser = await chromium.connectOverCDP(`http://${gateway}:9222/v/${index}`);
let disconnects = 0;
browser.on('disconnected', () => { disconnects++; log('!! browser connection closed'); });
const context = browser.contexts()[0] ?? (await browser.newContext());
const page = await context.newPage();
let pageEvents = 0;
page.on('close', () => { pageEvents++; log('!! page closed'); });
page.on('crash', () => { pageEvents++; log('!! page crashed'); });

await page.goto('about:blank');
const marker = await page.evaluate(() => {
  window.__tick = 0;
  setInterval(() => window.__tick++, 100); // the page's own clock
  window.__marker = 'page-' + Math.random().toString(36).slice(2, 8);
  return window.__marker;
});
log(`connected; page marker ${marker}; running ${seconds}s, one request every 100 ms`);

const stalls = [];
const errors = [];
let samples = 0, lastTick = -1, backwards = 0, markerChanged = 0;
let firstSkew = null, lastSkew = null; // lastSkew is also null before the first sample
let prevRecv = performance.now();
const end = Date.now() + Number(seconds) * 1000;

while (Date.now() < end) {
  try {
    const r = await Promise.race([
      page.evaluate(() => ({ tick: window.__tick, now: Date.now(), marker: window.__marker })),
      sleep(90_000).then(() => { throw new Error('no answer for 90 s'); }),
    ]);
    const recv = performance.now();
    const gap = recv - prevRecv;
    if (gap > 2000) { // normal cycles take ~100 ms of sleep plus the round trip to the gateway
      stalls.push({ at: stamp(), ms: Math.round(gap) });
      log(`stall: no answer for ${Math.round(gap)} ms, then the page replied (tick ${r.tick})`);
    }
    prevRecv = recv;
    samples++;
    if (r.tick < lastTick) backwards++;
    lastTick = r.tick;
    if (r.marker !== marker) markerChanged++;
    const skew = Date.now() - r.now; // agent clock minus guest clock
    if (lastSkew !== null && Math.abs(skew - lastSkew) > 500) {
      log(`guest clock offset changed: ${Math.round(lastSkew)} -> ${Math.round(skew)} ms`);
    }
    lastSkew = skew;
    firstSkew ??= skew;
  } catch (e) {
    errors.push(String(e.message ?? e).split('\n')[0]);
    log('!! request failed:', errors.at(-1));
    break;
  }
  await sleep(100);
}

const longest = stalls.reduce((m, s) => Math.max(m, s.ms), 0);
console.log('\n--- what the agent saw ---');
console.log(`requests answered       : ${samples}`);
console.log(`longest stall           : ${longest} ms   (${stalls.length} stall(s) over 2 s)`);
console.log(`connection closed events: ${disconnects}`);
console.log(`failed requests         : ${errors.length}`);
console.log(`page counter went back  : ${backwards} time(s); marker changed: ${markerChanged} time(s)`);
console.log(`guest clock behind agent: ${Math.round(firstSkew)} ms at start, ${Math.round(lastSkew)} ms at end`);

if (errors.length === 0) {
  await page.goto('https://checkip.amazonaws.com');
  console.log(`internet still reachable: yes, as ${(await page.textContent('body')).trim()}`);
}
const ok = disconnects === 0 && pageEvents === 0 && errors.length === 0 && backwards === 0 && markerChanged === 0;
console.log(ok ? 'RESULT: the connection and the page survived' : 'RESULT: something broke');
await page.close().catch(() => {});
await browser.close().catch(() => {});
process.exit(ok ? 0 : 1);
