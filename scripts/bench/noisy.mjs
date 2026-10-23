// Noisy neighbour: does a busy browser slow down another one on the same node?
// The victim times a fixed piece of JavaScript work inside its own page
// (performance.now(), so the network to the client doesn't matter), every
// 200 ms. The hog, if on, keeps two web workers spinning: both of its vCPUs
// busy. Prints p50/p95/p99 of the victim's task time as JSON.
// Usage: node noisy.mjs <gateway-ip> <victim index> <hog index> <hog on|off> [seconds] [label]
import { chromium } from 'playwright-core';

const [gateway, victimIdx, hogIdx, hog = 'off', seconds = '20', label = hog] = process.argv.slice(2);
const connect = (i) => chromium.connectOverCDP(`http://${gateway}:9222/v/${i}`);

let hogBrowser, hogPage;
if (hog === 'on') {
  hogBrowser = await connect(hogIdx);
  hogPage = await hogBrowser.contexts()[0].newPage();
  await hogPage.goto('about:blank');
  await hogPage.evaluate(() => {
    const src = URL.createObjectURL(new Blob(['for(;;){}'], { type: 'text/javascript' }));
    window.__hogs = [new Worker(src), new Worker(src)];
  });
  await new Promise((r) => setTimeout(r, 2000)); // let the hog reach full speed
}

const victim = await connect(victimIdx);
const page = await victim.contexts()[0].newPage();
await page.goto('about:blank');
const task = () => page.evaluate(() => {
  const t = performance.now();
  let x = 0;
  for (let i = 0; i < 3e6; i++) x += Math.sqrt(i); // a fixed amount of work
  return performance.now() - t + (x < 0 ? 1 : 0);
});
await task(); // warm the JIT
const samples = [];
const end = Date.now() + Number(seconds) * 1000;
while (Date.now() < end) {
  samples.push(await task());
  await new Promise((r) => setTimeout(r, 200));
}
const s = [...samples].sort((a, b) => a - b);
const q = (p) => +s[Math.min(s.length - 1, Math.floor(s.length * p))].toFixed(1);
console.log(JSON.stringify({ label, hog, n: s.length, p50_ms: q(0.5), p95_ms: q(0.95), p99_ms: q(0.99), max_ms: +s.at(-1).toFixed(1) }));

await page.close();
await victim.close();
if (hogPage) {
  await hogPage.close();
  await hogBrowser.close();
}
