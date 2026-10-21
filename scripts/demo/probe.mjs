// Connect to several VMs through the gateway and ask each the same questions:
// a random number, the clock, and what the internet sees. Clones of one base
// snapshot must differ in the first and agree on the other two.
// Usage: node probe.mjs <gateway-ip> <vm-index>...
import { chromium } from 'playwright-core';

const [gateway, ...indexes] = process.argv.slice(2);
if (!gateway || indexes.length === 0) {
  console.error('usage: node probe.mjs <gateway-ip> <vm-index>...');
  process.exit(2);
}
const results = await Promise.all(indexes.map(async (index) => {
  const browser = await chromium.connectOverCDP(`http://${gateway}:9222/v/${index}`);
  const context = browser.contexts()[0] ?? (await browser.newContext());
  const page = await context.newPage();
  const t0 = Date.now();
  const { uuid, now } = await page.evaluate(() => ({
    // getRandomValues works on any page; randomUUID needs a secure context.
    uuid: Array.from(crypto.getRandomValues(new Uint8Array(8)), (b) => b.toString(16).padStart(2, '0')).join(''),
    now: Date.now(),
  }));
  const offset = Date.now() - now; // this machine's clock minus the VM's, read right after the answer
  await page.goto('https://checkip.amazonaws.com');
  const egress = (await page.textContent('body')).trim();
  await page.close();
  await browser.close();
  return { index, uuid, clockOffsetMs: offset, egress, took: Date.now() - t0 };
}));
console.table(results);
const unique = new Set(results.map((r) => r.uuid)).size;
console.log(`${unique} distinct random values from ${results.length} VMs; egress addresses: ${[...new Set(results.map((r) => r.egress))].join(', ')}`);
