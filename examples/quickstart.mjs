// The quickstart from the README, runnable: BATON_API_KEY=bk_... node quickstart.mjs
import { Baton } from 'batonvm';
import { chromium } from 'playwright-core';

const baton = new Baton();
const session = await baton.sessions.create();
console.log(`session ${session.id} on ${session.node}`);
const browser = await chromium.connectOverCDP(session.connectUrl);
const context = browser.contexts()[0];
const page = context.pages()[0] ?? (await context.newPage());

await page.goto('https://example.com');
console.log('title:', await page.title());

try {
  const move = await session.migrate();
  console.log(`moved ${move.from} → ${move.to}, paused ${move.pausedMs.toFixed(0)} ms`);
  console.log('title after the move:', await page.title());
} catch (e) {
  console.log('migration not available right now:', e.message);
}

await session.close();
console.log('closed');
