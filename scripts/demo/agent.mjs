// A minimal agent: connect to Chrome in VM <n> through the gateway, load pages
// over the internet, take a screenshot.
// Usage: node agent.mjs <gateway-ip> [vm-index]
import { chromium } from 'playwright-core';

const [gateway, index = '1'] = process.argv.slice(2);
if (!gateway) {
  console.error('usage: node agent.mjs <gateway-ip> [vm-index]');
  process.exit(2);
}

const browser = await chromium.connectOverCDP(`http://${gateway}:9222/v/${index}`);
const context = browser.contexts()[0] ?? (await browser.newContext());
const page = await context.newPage();

// The VM's traffic leaves through the gateway, so the internet should see the
// gateway's address, whichever node the VM runs on.
await page.goto('https://checkip.amazonaws.com');
console.log('address the internet sees :', (await page.textContent('body')).trim());

await page.goto('https://example.com');
console.log('page title               :', await page.title());
await page.screenshot({ path: 'example.png' });
console.log('screenshot               : example.png');

await page.close();
await browser.close(); // disconnects; the browser keeps running in the VM
