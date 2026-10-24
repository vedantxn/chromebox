import bench from '@/data/benchmarks.json';
import arenaData from '@/data/arena.json';

// Everything the landing page states is read from measured results: our own
// benchmark file (scripts/bench/run.mjs, us-east-1), the Browser Arena run
// (bench/results/arena-baton-2026-10-24.jsonl), and, for other providers, the
// numbers Browser Arena itself published.

const t = bench.tests as unknown as Record<string, Record<string, { p50: number; samples?: number[] }>> & {
  migration: { paused: { p50: number; samples: number[] }; client_longest_wait: { p50: number; samples: number[] }; client_normal_round_trip: { p50: number }; disconnects: number };
};

// The twenty measured migrations: how long the browser was paused, and the
// longest a live client then waited for one reply.
export const moves = t.migration.paused.samples.map((paused, i) => ({
  paused: Math.round(paused),
  wait: Math.round(t.migration.client_longest_wait.samples[i]),
}));
export const normalReplyMs = t.migration.client_normal_round_trip.p50;

const ms = (v: number) => Math.round(v);
export const facts = {
  movePaused: ms(t.migration.paused.p50),
  moves: moves.length,
  disconnects: t.migration.disconnects as unknown as number,
  forkPaused: ms(t.fork_1.parent_paused.p50),
  fork3Ready: (t.fork_3.all_copies_ready.p50 / 1000).toFixed(1),
  pause: (t.pause_resume.pause_total.p50 / 1000).toFixed(1),
  resume: (t.pause_resume.resume_total.p50 / 1000).toFixed(1),
  profileLater: (t.profile.later_sessions.p50 / 1000).toFixed(1),
  profilePaused: ms((bench.tests.profile as unknown as { save: { browser_paused_ms: number } }).save.browser_paused_ms),
  profileMib: ms((bench.tests.profile as unknown as { save: { stored_mib: number } }).save.stored_mib),
};

// Browser Arena's lifecycle benchmark (create, connect, load example.com,
// release; the sum of the stage medians over 100 sessions in a row): our run
// with the warm pool, and the other providers' latest runs from the raw files
// Browser Arena publishes (scripts/bench/browserarena/peers.mjs).
type Run = { label: string; runs: number; successful: number; create: number; connect: number; load: number; release: number; total: number; p95: number };
type Peer = { provider: string; date: string; runs: number; successful: number; total: number };
const a = arenaData as unknown as { date: string; runs: Run[]; peers: Peer[] };
const ours = a.runs[a.runs.length - 1];
const round = (v: number) => Math.round(v);
export const arena = {
  baton: { create: round(ours.create), connect: round(ours.connect), load: round(ours.load), release: round(ours.release),
    total: round(ours.total), p95: round(ours.p95), runs: ours.runs, ok: ours.successful, date: a.date,
    before: round(a.runs[0].total) },
  published: a.peers.map((p) => ({ name: p.provider, total: round(p.total), date: p.date })),
};
