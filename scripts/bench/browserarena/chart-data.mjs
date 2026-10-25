// Builds web/src/data/arena-chart.json for the site's benchmark chart: per provider, the
// percentiles of whole sessions (create + connect + load + release) over successful runs, the
// way the Kestrel site computes them (linear interpolation). Ours from a results file of our own;
// the others from the latest one-at-a-time run of each in a clone of nottelabs/browserarena.
//
//   node chart-data.mjs path/to/browserarena path/to/our-results.jsonl
import fs from 'node:fs';
import path from 'node:path';

const [arenaRoot, ours] = process.argv.slice(2);
const KEYS = { create: 'session_creation_ms', connect: 'session_connect_ms', goto: 'page_goto_ms', release: 'session_release_ms' };
const pct = (values, q) => {
  const v = [...values].sort((a, b) => a - b), k = (v.length - 1) * q, f = Math.floor(k), c = Math.min(f + 1, v.length - 1);
  return v[f] + (v[c] - v[f]) * (k - f);
};
const read = (file) => fs.readFileSync(file, 'utf8').split('\n').filter(Boolean)
  .map((l) => { try { return JSON.parse(l); } catch { return null; } })
  .filter((r) => r && r.warmup !== true);
function summarize(rows) {
  const ok = rows.filter((r) => r.success === true && Object.values(KEYS).every((k) => typeof r[k] === 'number'));
  const totals = ok.map((r) => Object.values(KEYS).reduce((s, k) => s + r[k], 0));
  return {
    n: rows.length, successful: ok.length,
    phases: Object.fromEntries(Object.entries(KEYS).map(([name, k]) => [name, Math.round(pct(ok.map((r) => r[k]), 0.5))])),
    total: { p50: Math.round(pct(totals, 0.5)), p95: Math.round(pct(totals, 0.95)), p99: Math.round(pct(totals, 0.99)) },
  };
}

const labels = { tilion: 'Tilion', notte: 'Notte', 'kernel-headful': 'Kernel (headful)', kernel: 'Kernel', 'browser-use': 'Browser Use',
  lightpanda: 'Lightpanda', browserbase: 'Browserbase', cloudflare: 'Cloudflare', hyperbrowser: 'Hyperbrowser', steel: 'Steel',
  anchorbrowser: 'Anchor', 'aws-agentcore': 'AWS AgentCore', browserless: 'Browserless' };
const dir = path.join(arenaRoot, 'results', 'hello-browser');
const providers = [];
for (const id of fs.readdirSync(dir)) {
  const dates = fs.readdirSync(path.join(dir, id)).filter((d) => fs.existsSync(path.join(dir, id, d, 'c1', 'results.jsonl'))).sort();
  const date = dates.at(-1);
  if (!date) continue;
  const s = summarize(read(path.join(dir, id, date, 'c1', 'results.jsonl')));
  if (s.successful) providers.push({ id, label: labels[id] ?? id, note: 'hosted', date, ...s });
}
const mine = summarize(read(ours));
providers.push({ id: 'baton', label: 'Baton', note: 'hosted, us-east-1', date: path.basename(ours).match(/\d{4}-\d{2}-\d{2}/)?.[0] ?? '', ...mine });

fs.writeFileSync('web/src/data/arena-chart.json', JSON.stringify({ harness: 'Browser Arena', providers }, null, 2) + '\n');
for (const p of providers.sort((a, b) => a.total.p95 - b.total.p95)) console.log(`${p.label.padEnd(18)} p50 ${p.total.p50}  p95 ${p.total.p95}  p99 ${p.total.p99}  (${p.successful}/${p.n}, ${p.date})`);
