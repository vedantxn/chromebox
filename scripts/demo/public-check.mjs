// End-to-end check of the public data plane: create a VM through the API
// behind CloudFront, drive it over wss with its signed token, check that bad
// tokens are refused and that the VM cannot reach private networks.
// Usage: CHROMEBOX_TOKEN=... node public-check.mjs https://xxxx.cloudfront.net
import { chromium } from 'playwright-core';

const base = process.argv[2];
const auth = { Authorization: `Bearer ${process.env.CHROMEBOX_TOKEN}` };
const api = async (method, path, body) => {
  const r = await fetch(base + path, { method, headers: { ...auth, 'content-type': 'application/json' }, body: body && JSON.stringify(body) });
  if (!r.ok) throw new Error(`${method} ${path}: ${r.status} ${await r.text()}`);
  return r.status === 204 ? null : r.json();
};
const wss = base.replace('https://', 'wss://');

let t = Date.now();
const created = await api('POST', '/v1/vms', { ttl_seconds: 300 });
console.log(`created ${created.vm.id} on ${created.vm.node} in ${Date.now() - t} ms (server says ${created.total_ms.toFixed(0)} ms)`);

t = Date.now();
const browser = await chromium.connectOverCDP(`${wss}/cdp/${created.connect_token}`);
console.log(`connected over CloudFront in ${Date.now() - t} ms`);
const page = await (browser.contexts()[0] ?? (await browser.newContext())).newPage();
await page.goto('https://example.com');
console.log('the web works:', await page.title());

const tryGet = async (url) => {
  try { await page.goto(url, { timeout: 4000 }); return 'REACHED (bad)'; }
  catch (e) { return 'blocked'; }
};
for (const [what, url] of [
  ['instance metadata (AWS credentials)', 'http://169.254.169.254/latest/meta-data/'],
  ['the gateway inside the VPC', 'http://172.31.7.29:8080/healthz'],
  ["the node's agent via the VM's gateway", 'http://10.200.0.5:7000/v1/status'],
]) console.log(`${what.padEnd(40)} ${await tryGet(url)}`);
await browser.close();

for (const [what, tok] of [
  ['garbage token', 'abc.def'],
  ['token with a forged signature', created.connect_token.split('.')[0] + '.AAAA'],
]) {
  try { await chromium.connectOverCDP(`${wss}/cdp/${tok}`, { timeout: 5000 }); console.log(`${what}: ACCEPTED (bad)`); }
  catch (e) { console.log(`${what.padEnd(40)} refused`); }
}
const short = await api('POST', `/v1/vms/${created.vm.id}/token?kind=view&ttl=1`);
await new Promise((r) => setTimeout(r, 2000));
try { await chromium.connectOverCDP(`${wss}/cdp/${short.token}`, { timeout: 5000 }); console.log('expired token: ACCEPTED (bad)'); }
catch (e) { console.log(`${'expired token'.padEnd(40)} refused`); }

await api('DELETE', `/v1/vms/${created.vm.id}`);
console.log('deleted');
