// Summarize a browserbench results file (JSON lines) the way Steel's queries do:
// successful runs only, total = create + connect + goto + release, quantiles
// interpolated linearly (DuckDB's quantile_cont).
//
//   node summarize.mjs ../../../bench/results/browserbench-baton-2026-10-24.jsonl > summary.json
import fs from 'node:fs';

const rows = fs.readFileSync(process.argv[2], 'utf8').trim().split('\n').map((l) => JSON.parse(l));
const ok = rows.filter((r) => r.success);
const stages = {
  create: ok.map((r) => r.session_creation_ms),
  connect: ok.map((r) => r.session_connect_ms),
  goto: ok.map((r) => r.page_goto_ms),
  release: ok.map((r) => r.session_release_ms),
  total: ok.map((r) => r.session_creation_ms + r.session_connect_ms + r.page_goto_ms + r.session_release_ms),
};
const quantile = (sorted, p) => {
  const i = (sorted.length - 1) * p, lo = Math.floor(i), hi = Math.ceil(i);
  return sorted[lo] + (sorted[hi] - sorted[lo]) * (i - lo);
};
const r2 = (x) => Math.round(x * 100) / 100;
const out = { runs: rows.length, success: ok.length, failures: rows.filter((r) => !r.success).map((r) => ({ stage: r.error_stage, message: String(r.error_message).split('\n')[0] })), stages: {} };
for (const [name, xs] of Object.entries(stages)) {
  const s = [...xs].sort((a, b) => a - b);
  out.stages[name] = { avg: r2(s.reduce((a, b) => a + b, 0) / s.length), p50: r2(quantile(s, 0.5)), p95: r2(quantile(s, 0.95)), p99: r2(quantile(s, 0.99)), min: s[0], max: s.at(-1) };
}
console.log(JSON.stringify(out, null, 2));
