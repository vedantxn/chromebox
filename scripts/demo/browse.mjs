// The headline demo's agent: it clicks through Wikipedia, one article to the
// next, for a while, and reports every step. Meant to run while the node under
// its browser is drained: the steps keep coming, nothing fails, and a marker
// kept in the tab's sessionStorage (page state, in the browser's memory) is
// still there at the end.
// Usage: node browse.mjs <gateway-ip> [vm-index] [seconds]
import { chromium } from 'playwright-core';

const [gateway, index = '1', seconds = '60'] = process.argv.slice(2);
if (!gateway) {
  console.error('usage: node browse.mjs <gateway-ip> [vm-index] [seconds]');
  process.exit(2);
}
const stamp = () => new Date().toISOString().slice(11, 23);
const browser = await chromium.connectOverCDP(`http://${gateway}:9222/v/${index}`);
let closed = false;
browser.on('disconnected', () => { closed = true; console.log(stamp(), '!! browser connection closed'); });
const context = browser.contexts()[0] ?? (await browser.newContext());
const page = await context.newPage();

const start = 'https://en.wikipedia.org/wiki/Live_migration';
await page.goto(start, { waitUntil: 'domcontentloaded' });
const marker = await page.evaluate(() => (sessionStorage.marker = 'agent-' + Math.random().toString(36).slice(2, 8)));
console.log(stamp(), `start: ${await page.title()}  (marker ${marker} in sessionStorage)`);

const steps = [];
let errors = 0;
const end = Date.now() + Number(seconds) * 1000;
while (Date.now() < end && !closed) {
  const t = performance.now();
  try {
    // Mark the article links in the body text (not special pages such as
    // File: or Help:, which have a colon in their path), then click one.
    const n = await page.evaluate(() => {
      const as = [...document.querySelectorAll('#mw-content-text p a')]
        .filter((a) => a.pathname.startsWith('/wiki/') && !a.pathname.slice(6).includes(':') && !a.hash);
      as.forEach((a, i) => a.setAttribute('data-step', i));
      return as.length;
    });
    const links = page.locator('[data-step]');
    if (n === 0) {
      await page.goto(start, { waitUntil: 'domcontentloaded' });
      continue;
    }
    await Promise.all([
      page.waitForNavigation({ waitUntil: 'domcontentloaded', timeout: 30_000 }),
      links.nth(Math.floor(Math.random() * Math.min(n, 15))).click(),
    ]);
    const ms = Math.round(performance.now() - t);
    steps.push(ms);
    console.log(stamp(), `step ${String(steps.length).padStart(3)}  ${String(ms).padStart(5)} ms  ${await page.title()}`);
  } catch (e) {
    errors++;
    console.log(stamp(), '!! step failed:', String(e.message ?? e).split('\n')[0]);
  }
}

const kept = await page.evaluate(() => sessionStorage.marker).catch(() => null);
const sorted = [...steps].sort((a, b) => a - b);
console.log('\n--- the agent ---');
console.log(`steps                : ${steps.length}, failed ${errors}`);
console.log(`step time            : median ${sorted[sorted.length >> 1]} ms, slowest ${sorted.at(-1)} ms`);
console.log(`connection           : ${closed ? 'CLOSED' : 'open the whole time'}`);
console.log(`page state (marker)  : ${kept === marker ? 'kept' : `LOST (${kept})`}`);
await page.close().catch(() => {});
await browser.close().catch(() => {});
