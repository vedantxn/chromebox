// Browser Arena's numbers for one provider's results file, computed the way its
// leaderboard does (nottelabs/browserarena at d7992c6: web/lib/data.ts and
// web/lib/value-score.ts):
//   - measured sessions only (warm-ups are flagged in the file and ignored);
//   - latency = the sum of the phase medians over successful sessions;
//   - the tail is the p95 (nearest rank) of per-session totals;
//   - value score = weighted mean of three scores on fixed anchors: latency
//     (10,000 ms -> 0, 0 ms -> 1; 70% the median total, 30% the p95 total),
//     reliability (90% -> 0, 100% -> 1), cost ($0.20/h -> 0, $0 -> 1).
//
//   node score.mjs results.jsonl [price-per-hour ...]
import fs from 'node:fs';

const rows = fs.readFileSync(process.argv[2], 'utf8').trim().split('\n').map((l) => JSON.parse(l)).filter((r) => r.warmup !== true);
const prices = process.argv.slice(3).map(Number);

const median = (xs) => { if (!xs.length) return 0; const s = [...xs].sort((a, b) => a - b), m = s.length >> 1; return s.length % 2 ? s[m] : (s[m - 1] + s[m]) / 2; };
const percentile = (xs, p) => { if (!xs.length) return 0; const s = [...xs].sort((a, b) => a - b); return s[Math.max(0, Math.min(Math.ceil(p * s.length) - 1, s.length - 1))]; };
const ok = rows.filter((r) => r.success);
const col = (k) => ok.map((r) => r[k] ?? 0);
const phases = { create: median(col('session_creation_ms')), connect: median(col('session_connect_ms')), load: median(col('page_goto_ms')), release: median(col('session_release_ms')) };
const latency = phases.create + phases.connect + phases.load + phases.release;
const p95 = percentile(ok.map((r) => (r.session_creation_ms ?? 0) + (r.session_connect_ms ?? 0) + (r.page_goto_ms ?? 0) + (r.session_release_ms ?? 0)), 0.95);
const reliability = (ok.length / rows.length) * 100;

const clamp = (v) => Math.max(0, Math.min(1, v));
const normLat = (ms) => clamp((10000 - ms) / 10000);
const latScore = (head, tail) => 0.7 * normLat(head) + 0.3 * normLat(tail);
const relScore = (r) => clamp((r - 90) / 10);
const costScore = (c) => (c == null ? 0 : clamp((0.2 - c) / 0.2));
const PRESETS = { Fastest: [100, 0, 0], Cheapest: [0, 0, 100], 'Most reliable': [5, 90, 5], Balanced: [33, 33, 33] };
const value = (w, c) => { const t = w[0] + w[1] + w[2]; return (w[0] * latScore(latency, p95) + w[1] * relScore(reliability) + w[2] * costScore(c)) / t; };

console.log(JSON.stringify({ runs: rows.length, successful: ok.length, reliability_pct: +reliability.toFixed(1), phase_medians_ms: phases, latency_ms: Math.round(latency), p95_total_ms: Math.round(p95),
  latency_score: +latScore(latency, p95).toFixed(3), reliability_score: +relScore(reliability).toFixed(3) }, null, 2));
const header = ['price/hr', ...Object.keys(PRESETS)];
console.log(header.join('\t'));
for (const c of [null, ...prices]) console.log([c == null ? 'unpriced' : `$${c.toFixed(2)}`, ...Object.values(PRESETS).map((w) => value(w, c).toFixed(3))].join('\t'));
