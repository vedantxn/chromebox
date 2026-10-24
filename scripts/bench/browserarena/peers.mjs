// Per-stage medians for the providers whose raw results Browser Arena publishes
// (results/hello-browser/<provider>/<date>/c1/results.jsonl in a clone of
// nottelabs/browserarena), for the latest date of each, computed as score.mjs does.
//
//   node peers.mjs path/to/browserarena > peers.json
import fs from 'node:fs';
import path from 'node:path';

const root = path.join(process.argv[2], 'results', 'hello-browser');
const median = (xs) => { const s = [...xs].sort((a, b) => a - b), m = s.length >> 1; return s.length % 2 ? s[m] : (s[m - 1] + s[m]) / 2; };
const out = [];
for (const provider of fs.readdirSync(root)) {
  const dates = fs.readdirSync(path.join(root, provider)).filter((d) => fs.existsSync(path.join(root, provider, d, 'c1', 'results.jsonl'))).sort();
  const date = dates.at(-1);
  if (!date) continue;
  const rows = fs.readFileSync(path.join(root, provider, date, 'c1', 'results.jsonl'), 'utf8').trim().split('\n')
    .map((l) => { try { return JSON.parse(l); } catch { return null; } }).filter((r) => r && r.warmup !== true);
  const ok = rows.filter((r) => r.success);
  if (!ok.length) continue;
  const m = (k) => median(ok.map((r) => r[k] ?? 0));
  const stages = { create: m('session_creation_ms'), connect: m('session_connect_ms'), load: m('page_goto_ms'), release: m('session_release_ms') };
  out.push({ provider, date, runs: rows.length, successful: ok.length, ...stages, total: stages.create + stages.connect + stages.load + stages.release });
}
out.sort((a, b) => a.total - b.total);
console.log(JSON.stringify(out, null, 2));
