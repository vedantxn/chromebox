// What a fork copies and what it must not. Two phases:
//   setup <gateway> <index>         the parent: log in (a session cookie set by
//                                   httpbin.org) and keep a marker in localStorage
//   check <gateway> <index> ...     each VM (parent and copies): in the tab that
//                                   was open at the fork, read the cookie and
//                                   the marker, ask the server which cookie it
//                                   sees, and draw random numbers in that tab
//                                   and in a brand-new tab.
// Same cookie and marker everywhere = the state was forked. Same random
// numbers in the old tab = that tab's generator was copied too.
import { chromium } from 'playwright-core';

const [phase, gateway, ...indexes] = process.argv.slice(2);
const connect = (i) => chromium.connectOverCDP(`http://${gateway}:9222/v/${i}`);

if (phase === 'setup') {
  const browser = await connect(indexes[0]);
  const page = await browser.contexts()[0].newPage();
  const session = 'user-' + Math.random().toString(36).slice(2, 8);
  await page.goto(`https://httpbin.org/cookies/set?session=${session}`);
  await page.evaluate((s) => localStorage.setItem('marker', s), session);
  console.log(`parent logged in as ${session}: ${await page.textContent('body')}`.replace(/\s+/g, ' '));
  await browser.close(); // disconnects; the tab stays open in the VM
} else if (phase === 'check') {
  const rows = [];
  for (const i of indexes) {
    const browser = await connect(i);
    const ctx = browser.contexts()[0];
    const page = ctx.pages().find((p) => p.url().includes('httpbin.org'));
    if (!page) { rows.push({ vm: i, error: 'no httpbin tab' }); continue; }
    const old = await page.evaluate(async () => ({
      cookie: document.cookie,
      marker: localStorage.getItem('marker'),
      server: (await (await fetch('/cookies')).json()).cookies.session,
      mathRandom: Math.random().toFixed(8),
      crypto: crypto.getRandomValues(new Uint32Array(1))[0],
    }));
    const fresh = await ctx.newPage();
    await fresh.goto('https://httpbin.org/get');
    const freshRandom = await fresh.evaluate(() => Math.random().toFixed(8));
    await fresh.close();
    rows.push({ vm: i, ...old, freshTabRandom: freshRandom });
    await browser.close();
  }
  console.table(rows);
  const same = (k) => new Set(rows.map((r) => r[k])).size === 1;
  console.log(`cookie the server sees : ${same('server') ? 'the same in all (the session was forked)' : 'DIFFERENT'}`);
  console.log(`localStorage marker    : ${same('marker') ? 'the same in all' : 'DIFFERENT'}`);
  console.log(`Math.random, old tab   : ${same('mathRandom') ? 'IDENTICAL in every VM (the generator was copied)' : 'different'}`);
  console.log(`crypto, old tab        : ${same('crypto') ? 'IDENTICAL in every VM' : 'different'}`);
  console.log(`Math.random, new tab   : ${same('freshTabRandom') ? 'IDENTICAL in every VM' : 'different'}`);
} else {
  console.error('usage: node fork-probe.mjs setup|check <gateway> <index>...');
  process.exit(2);
}
