// Builds web/src/data/arena.json for /benchmarks from our Browser Arena runs and
// peers.json (from peers.mjs), and copies the raw runs to web/public/.
//
//   node page-data.mjs peers.json <date> <label>=<results.jsonl> ...
import fs from 'node:fs';
import path from 'node:path';

const [peersFile, date, ...runArgs] = process.argv.slice(2);
const median = (xs) => { const s = [...xs].sort((a, b) => a - b), m = s.length >> 1; return s.length % 2 ? s[m] : (s[m - 1] + s[m]) / 2; };
const p95 = (xs) => { const s = [...xs].sort((a, b) => a - b); return s[Math.max(0, Math.min(Math.ceil(0.95 * s.length) - 1, s.length - 1))]; };
const runs = runArgs.map((arg) => {
  const [label, file] = arg.split('=');
  const rows = fs.readFileSync(file, 'utf8').trim().split('\n').map((l) => JSON.parse(l)).filter((r) => r.warmup !== true);
  const ok = rows.filter((r) => r.success);
  const m = (k) => median(ok.map((r) => r[k] ?? 0));
  const stages = { create: m('session_creation_ms'), connect: m('session_connect_ms'), load: m('page_goto_ms'), release: m('session_release_ms') };
  const out = path.basename(file).startsWith('arena-') ? path.basename(file) : `arena-${path.basename(file)}`;
  fs.copyFileSync(file, path.join('web/public', out));
  return { label, file: out, runs: rows.length, successful: ok.length, ...stages,
    total: stages.create + stages.connect + stages.load + stages.release,
    p95: p95(ok.map((r) => r.session_creation_ms + r.session_connect_ms + r.page_goto_ms + r.session_release_ms)) };
});
const names = { tilion: 'Tilion', notte: 'Notte', 'kernel-headful': 'Kernel (headful)', kernel: 'Kernel', 'browser-use': 'Browser Use',
  lightpanda: 'Lightpanda', browserbase: 'Browserbase', cloudflare: 'Cloudflare', hyperbrowser: 'Hyperbrowser', steel: 'Steel',
  anchorbrowser: 'Anchor Browser', 'aws-agentcore': 'AWS AgentCore', browserless: 'Browserless' };
const peers = JSON.parse(fs.readFileSync(peersFile, 'utf8')).map((p) => ({ ...p, provider: names[p.provider] ?? p.provider }));
fs.writeFileSync('web/src/data/arena.json', JSON.stringify({ date, client: 't3.small in us-east-1, on a free account', runs, peers }, null, 2) + '\n');
for (const r of runs) console.log(`${r.label.padEnd(28)} ${r.successful}/${r.runs}  total ${r.total} ms  p95 ${r.p95}`);
