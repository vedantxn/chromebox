// N agents, one per forked copy, working at the same time. Each picks up the
// tab that was open at the fork (already logged in), does its own errand, and
// checks it is still the same user.
// Usage: node fork-agents.mjs <gateway-ip> <index>...
import { chromium } from 'playwright-core';

const [gateway, ...indexes] = process.argv.slice(2);
const stamp = () => new Date().toISOString().slice(11, 23);
const t0 = Date.now();
const results = await Promise.all(indexes.map(async (i, n) => {
  const browser = await chromium.connectOverCDP(`http://${gateway}:9222/v/${i}`);
  const page = browser.contexts()[0].pages().find((p) => p.url().includes('httpbin.org'));
  const startedOn = page.url();
  // Its own errand: a different request per agent, from the same logged-in tab.
  await page.goto(`https://httpbin.org/anything?agent=${n + 1}&task=errand-${n + 1}`);
  const errand = JSON.parse(await page.textContent('body')).args.task;
  await page.goto('https://httpbin.org/cookies');
  const user = JSON.parse(await page.textContent('body')).cookies.session;
  console.log(stamp(), `agent ${n + 1} (VM ${i}): started on ${startedOn}, did ${errand}, still logged in as ${user}`);
  await browser.close();
  return user;
}));
const users = new Set(results);
console.log(`\n${results.length} agents in ${Date.now() - t0} ms, all as ${[...users].join(', ')}${users.size === 1 ? '' : ' (DIFFERENT USERS)'}`);
