// Baton benchmarks. Runs on the bench client in us-east-1 (scripts/aws/bench-client.sh)
// and prints one JSON document: the environment, and for each test its raw
// samples and p50/p95/p99. Tests (all by default, or name them):
//
//   fresh      orchestrator create: request -> browser answering, routed (n=100)
//   public     public API create -> connectOverCDP -> first page load -> end (n=40),
//              through Vercel and CloudFront, as a customer
//   migration  post-copy migrations of a VM under a live CDP client (n=20)
//   fork       fork a browser into 1 and into 3 copies (n=5 each)
//   density    memory per fresh VM and per fork copy (3 of each)
//   pause      pause a browser to S3 and resume it, 10 round trips
//   profile    save a profile, then start sessions from it (needs KEY)
//
// Environment: ORCH (orchestrator URL), GW (gateway private IP), TOKEN (orchestrator
// token), API (public site), KEY (an API key, for "public").
import { chromium } from 'playwright-core';

const { ORCH, GW, TOKEN, API, KEY } = process.env;
const FRESH_GAP_MS = Number(process.env.FRESH_GAP_MS ?? 0); // pause after each delete in the fresh test
const want = process.argv.slice(2);
const run = (t) => want.length === 0 || want.includes(t);
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const log = (...a) => console.error(new Date().toISOString().slice(11, 19), ...a);

async function orch(method, path, body) {
  const r = await fetch(ORCH + path, {
    method, headers: { Authorization: `Bearer ${TOKEN}`, 'content-type': 'application/json' },
    body: body && JSON.stringify(body),
  });
  const text = await r.text();
  if (!r.ok) throw new Error(`${method} ${path}: ${r.status} ${text.trim()}`);
  return text ? JSON.parse(text) : null;
}
const create = () => orch('POST', '/v1/vms', { base: 'chrome-v2' });
const del = (id) => orch('DELETE', `/v1/vms/${id}`).catch(() => {});
const list = () => orch('GET', '/v1/vms');
const cdp = (vm) => `http://${GW}:9222/v/${vm.net_index}`;

function summary(xs) {
  const s = [...xs].sort((a, b) => a - b);
  const q = (p) => (s.length ? +s[Math.min(s.length - 1, Math.floor(s.length * p))].toFixed(1) : null);
  return { n: s.length, p50: q(0.5), p95: q(0.95), p99: q(0.99), min: s.length ? +s[0].toFixed(1) : null, max: s.length ? +s.at(-1).toFixed(1) : null };
}
const result = (unit, samples) => ({ unit, ...summary(samples), samples: samples.map((x) => +x.toFixed(1)) });

const out = { started_at: new Date().toISOString(), tests: {} };

// The environment, as the nodes report it.
const nodes = await orch('GET', '/v1/nodes');
out.environment = {
  region: 'us-east-1',
  client: 'bench client, t3.small, same VPC',
  nodes: 'm8i.large (2 vCPU, 8 GiB) with nested virtualization; gateway t3.small',
  vmm: 'Firecracker v1.17.0 under its jailer; cgroup v2 per VM (guest + 256 MiB; capped at 1 CPU once running); 2 pre-started jailed VMMs kept ready per node',
  guest: 'Chrome for Testing 156, 2 vCPU, 1 GiB per browser',
  nodes_up: nodes.filter((n) => n.up).map((n) => n.name),
};

if (run('fresh')) {
  log('fresh: 100 creates');
  const total = [];
  for (let i = 0; i < 100; i++) {
    const r = await create();
    total.push(r.total_ms);
    await del(r.vm.id);
    if (FRESH_GAP_MS) await sleep(FRESH_GAP_MS); // a deleted VM finishes being removed in the background
  }
  out.tests.fresh = { what: 'orchestrator create: request -> Chrome answering CDP, gateway route set (server-side time)', ...result('ms', total) };
}

if (run('public') && KEY) {
  log('public: 40 sessions through the public API');
  const h = { Authorization: `Bearer ${KEY}` };
  const createMs = [], connectMs = [], loadMs = [], totalMs = [];
  for (let i = 0; i < 40; i++) {
    const start = Date.now();
    let t = performance.now();
    const r = await fetch(`${API}/api/v1/sessions`, { method: 'POST', headers: h });
    const s = await r.json();
    if (!r.ok) { log('public create failed', r.status, s); await sleep(10_000); continue; }
    createMs.push(performance.now() - t);
    t = performance.now();
    const browser = await chromium.connectOverCDP(s.connectUrl);
    connectMs.push(performance.now() - t);
    t = performance.now();
    const page = browser.contexts()[0].pages()[0] ?? (await browser.contexts()[0].newPage());
    await page.goto('https://example.com');
    loadMs.push(performance.now() - t);
    totalMs.push(createMs.at(-1) + connectMs.at(-1) + loadMs.at(-1));
    await fetch(`${API}/api/v1/sessions/${s.id}`, { method: 'DELETE', headers: h });
    await browser.close().catch(() => {});
    await sleep(Math.max(0, 6500 - (Date.now() - start))); // the account limit: 10 creates a minute
  }
  out.tests.public = {
    what: 'a customer in us-east-1: POST /api/v1/sessions (Vercel) -> connectOverCDP (CloudFront) -> load example.com',
    create: result('ms', createMs), connect: result('ms', connectMs), first_page: result('ms', loadMs), total: result('ms', totalMs),
  };
}

if (run('migration')) {
  log('migration: 20 moves under a live client');
  const r = await create();
  const vm = r.vm;
  const browser = await chromium.connectOverCDP(cdp(vm));
  const page = browser.contexts()[0].pages()[0] ?? (await browser.contexts()[0].newPage());
  await page.goto('about:blank');
  let closed = false;
  browser.on('disconnected', () => { closed = true; });
  // The client: one CDP request at a time, as fast as answers come; the
  // longest wait around each move is what an agent would feel.
  const answers = [];
  let stop = false;
  const client = (async () => {
    while (!stop && !closed) {
      try { await page.evaluate(() => 1); answers.push(performance.now()); } catch { break; }
    }
  })();
  await sleep(2000);
  const paused = [], unreachable = [], stream = [], longestWait = [];
  for (let i = 0; i < 20; i++) {
    const t0 = performance.now();
    const m = await orch('POST', `/v1/vms/${vm.id}/migrate`);
    await sleep(1500);
    const t1 = performance.now();
    const inWindow = answers.filter((a) => a >= t0 - 500 && a <= t1);
    let gap = 0;
    for (let j = 1; j < inWindow.length; j++) gap = Math.max(gap, inWindow[j] - inWindow[j - 1]);
    paused.push(m.paused_ms); unreachable.push(m.unreachable_ms); stream.push(m.stream_ms); longestWait.push(gap);
    await sleep(1000);
  }
  const between = [];
  for (let j = 1; j < Math.min(answers.length, 200); j++) between.push(answers[j] - answers[j - 1]);
  stop = true;
  await client;
  out.tests.migration = {
    what: 'post-copy migration between two nodes while a CDP client sends requests back to back',
    paused: result('ms', paused), unreachable: result('ms', unreachable), memory_streamed_after: result('ms', stream),
    client_longest_wait: result('ms', longestWait), client_normal_round_trip: summary(between),
    disconnects: closed ? 1 : 0,
  };
  await browser.close().catch(() => {});
  await del(vm.id);
}

if (run('fork')) {
  for (const n of [1, 3]) {
    log(`fork: into ${n}, 5 times`);
    const paused = [], total = [];
    for (let i = 0; i < 5; i++) {
      const parent = (await create()).vm;
      const b = await chromium.connectOverCDP(cdp(parent));
      const p = b.contexts()[0].pages()[0] ?? (await b.contexts()[0].newPage());
      await p.goto('https://example.com');
      await b.close();
      const f = await orch('POST', `/v1/vms/${parent.id}/fork`, { count: n });
      paused.push(f.paused_ms); total.push(f.total_ms);
      for (const c of f.copies) await del(c.id);
      await del(parent.id);
      await sleep(20_000); // let the image be collected (15 s grace)
    }
    out.tests[`fork_${n}`] = {
      what: `fork a browser with a page open into ${n} cop${n === 1 ? 'y' : 'ies'}: parent pause, and request -> all copies answering (each copy also reopens its tab in a fresh renderer)`,
      parent_paused: result('ms', paused), all_copies_ready: result('ms', total),
    };
  }
}

if (run('density')) {
  // On one node (the caller cordons the others), so that shared pages are
  // shared: a page mapped by a single process counts as that process's own.
  log('density');
  const fresh = [];
  for (let i = 0; i < 3; i++) fresh.push((await create()).vm);
  await sleep(10_000);
  let vms = await list();
  const pick = (ids, k) => vms.filter((v) => ids.includes(v.id)).map((v) => v[k]);
  const freshIds = fresh.map((v) => v.id);
  const freshPrivate = pick(freshIds, 'private_mib'), freshPss = pick(freshIds, 'pss_mib');
  for (const v of fresh.slice(1)) await del(v.id); // make room for the copies
  const parent = fresh[0];
  const b = await chromium.connectOverCDP(cdp(parent));
  const p = b.contexts()[0].pages()[0] ?? (await b.contexts()[0].newPage());
  await p.goto('https://example.com');
  await b.close();
  const f = await orch('POST', `/v1/vms/${parent.id}/fork`, { count: 3 });
  await sleep(10_000);
  vms = await list();
  const copyIds = f.copies.map((c) => c.id);
  out.tests.density = {
    what: 'memory a browser really costs on its node: private is its own, PSS adds its share of pages shared with others (3 fresh browsers on one node; then one of them forked into 3 copies on that node)',
    fresh_private_mib: freshPrivate, fresh_pss_mib: freshPss,
    fork_copy_private_mib: pick(copyIds, 'private_mib'), fork_copy_pss_mib: pick(copyIds, 'pss_mib'),
    fork_image_mib: f.image_mib, guest_memory_mib: 1024, fork_errors: f.errors ?? [],
  };
  for (const v of [parent, ...f.copies]) await del(v.id);
}

if (run('pause')) {
  log('pause and resume: 10 round trips');
  const vm = (await create()).vm;
  const b = await chromium.connectOverCDP(cdp(vm));
  const pg = b.contexts()[0].pages()[0] ?? (await b.contexts()[0].newPage());
  await pg.goto('https://example.com');
  await b.close();
  const paused = [], upload = [], pauseTotal = [], resumeTotal = [], download = [], stored = [];
  for (let i = 0; i < 10; i++) {
    const p = await orch('POST', `/v1/vms/${vm.id}/pause`);
    paused.push(p.paused_ms); upload.push(p.upload_ms); pauseTotal.push(p.total_ms); stored.push(p.stored_mib);
    await sleep(1000);
    const r = await orch('POST', `/v1/vms/${vm.id}/resume`);
    resumeTotal.push(r.total_ms); download.push(r.download_ms);
    await sleep(1000);
  }
  out.tests.pause_resume = {
    what: 'pause a Chrome with a page open to S3 and resume it (the node may differ each time), 10 round trips; the guest clock is set on resume',
    pause_total: result('ms', pauseTotal), browser_stopped_for_snapshot: result('ms', paused), upload: result('ms', upload),
    resume_total: result('ms', resumeTotal), resume_download: result('ms', download), stored_mib: result('MiB', stored),
  };
  await del(vm.id);
}

if (run('profile') && KEY) {
  log('profile: save, then 8 sessions from it');
  const h = { Authorization: `Bearer ${KEY}`, 'content-type': 'application/json' };
  const api = async (method, path, body) => {
    const r = await fetch(`${API}/api/v1${path}`, { method, headers: h, body: body && JSON.stringify(body) });
    const text = await r.text();
    if (!r.ok) throw new Error(`${method} ${path}: ${r.status} ${text}`);
    return text ? JSON.parse(text) : null;
  };
  const s = await api('POST', '/sessions');
  const b = await chromium.connectOverCDP(s.connectUrl);
  const pg = b.contexts()[0].pages()[0] ?? (await b.contexts()[0].newPage());
  await pg.goto('https://httpbin.org/cookies/set?session=bench');
  await b.close();
  const t0 = performance.now();
  const saved = await api('POST', '/profiles', { session: s.id, name: 'bench' });
  const saveMs = performance.now() - t0;
  await api('DELETE', `/sessions/${s.id}`);
  const first = [], later = [];
  for (let i = 0; i < 8; i++) {
    const start = Date.now();
    const t = performance.now();
    const n = await api('POST', '/sessions', { profile: 'bench' });
    (i === 0 ? first : later).push(performance.now() - t);
    await api('DELETE', `/sessions/${n.id}`);
    await sleep(Math.max(0, 6500 - (Date.now() - start)));
  }
  await api('DELETE', '/profiles/bench');
  out.tests.profile = {
    what: 'save a logged-in browser as a profile, then start sessions from it through the public API (create request to ready; the first fetches the profile to a node)',
    save: { ms: +saveMs.toFixed(0), browser_paused_ms: saved.pausedMs, stored_mib: saved.storedMiB },
    first_session: result('ms', first), later_sessions: result('ms', later),
  };
}

out.finished_at = new Date().toISOString();
console.log(JSON.stringify(out, null, 2));
