// Log in once, save the browser as a profile, and start new sessions that are
// already logged in, through the public API and the SDK only.
// Usage: BATON_API_KEY=bk_... node profile-demo.mjs
import { Baton } from 'batonvm';
import { chromium } from 'playwright-core';

const baton = new Baton();
const t = () => performance.now();
const read = async (s) => {
  const b = await chromium.connectOverCDP(s.connectUrl);
  const pg = b.contexts()[0].pages().find((x) => x.url().includes('httpbin'));
  const r = pg
    ? await pg.evaluate(async () => ({ page: location.pathname, cookie: (await (await fetch('/cookies')).json()).cookies, cart: localStorage.getItem('cart') }))
    : 'no httpbin page';
  await b.close();
  return r;
};

// 1. Log in once.
const s1 = await baton.sessions.create();
const b1 = await chromium.connectOverCDP(s1.connectUrl);
const pg1 = b1.contexts()[0].pages()[0] ?? (await b1.contexts()[0].newPage());
await pg1.goto('https://httpbin.org/cookies/set?session=alice-the-user');
await pg1.evaluate(() => localStorage.setItem('cart', '3 items'));
let t0 = t();
const saved = await s1.saveProfile('shop');
console.log(`profile saved in ${Math.round(t() - t0)} ms (the browser paused ${Math.round(saved.pausedMs)} ms; ${Math.round(saved.storedMiB)} MiB stored)`);
console.log('the original session is still:', (await baton.sessions.get(s1.id)).status);
await b1.close();
await s1.close();
console.log('profiles:', JSON.stringify((await baton.profiles.list()).map((p) => p.name)));

// 2. Later: new sessions start logged in.
let keep;
for (const label of ['first session from the profile', 'second session from the profile']) {
  t0 = t();
  const s = await baton.sessions.create({ profile: 'shop' });
  const ms = Math.round(t() - t0);
  console.log(`${label}: ready in ${ms} ms on ${s.node}:`, JSON.stringify(await read(s)));
  if (label.startsWith('second')) keep = s;
  else await s.close();
}
await keep.close();
try {
  await baton.sessions.create({ profile: 'nope' });
} catch (e) {
  console.log('unknown profile:', e.status, e.message);
}
await baton.profiles.delete('shop');
console.log('after delete:', JSON.stringify(await baton.profiles.list()));
