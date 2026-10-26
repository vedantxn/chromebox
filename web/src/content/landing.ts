// The landing page's words and facts. Every number is read from a measured results file: our own
// benchmark run (data/benchmarks.json, scripts/bench/run.mjs in us-east-1), our Browser Arena runs
// (data/arena.json) and the chart of every provider's whole sessions (data/arena-chart.json, built
// by scripts/bench/browserarena/chart-data.mjs from Browser Arena's raw files and ours).
import benchJson from "@/data/benchmarks.json";
import arenaJson from "@/data/arena.json";
import chartJson from "@/data/arena-chart.json";

type Sample = { p50: number; samples?: number[] };
const t = benchJson.tests as unknown as Record<string, Record<string, Sample>> & {
  migration: {
    paused: Sample & { samples: number[] };
    unreachable: Sample;
    client_longest_wait: Sample;
    disconnects: number;
  };
  profile: { save: { browser_paused_ms: number; stored_mib: number } } & Record<string, Sample>;
};

const ms = Math.round;
const s = (v: number) => (v / 1000).toFixed(1);

export const facts = {
  movePaused: ms(t.migration.paused.p50),
  moveUnreachable: ms(t.migration.unreachable.p50),
  moveLongestWait: ms(t.migration.client_longest_wait.p50),
  moves: t.migration.paused.samples.length,
  disconnects: t.migration.disconnects,
  forkPaused: ms(t.fork_1.parent_paused.p50),
  fork3Ready: s(t.fork_3.all_copies_ready.p50),
  pause: s(t.pause_resume.pause_total.p50),
  resume: s(t.pause_resume.resume_total.p50),
  profileLater: s(t.profile.later_sessions.p50),
  profilePaused: ms(t.profile.save.browser_paused_ms),
  profileMib: ms(t.profile.save.stored_mib),
};

type Run = {
  label: string;
  runs: number;
  successful: number;
  create: number;
  connect: number;
  load: number;
  release: number;
  total: number;
  p95: number;
};
const runs = (arenaJson as unknown as { date: string; client: string; runs: Run[] }).runs;
const last = runs[runs.length - 1];

/** Our latest Browser Arena run: the median of each stage over 100 sessions in a row. */
export const arena = {
  date: arenaJson.date,
  client: arenaJson.client,
  runs: last.runs,
  successful: last.successful,
  phases: {
    create: ms(last.create),
    connect: ms(last.connect),
    goto: ms(last.load),
    release: ms(last.release),
  },
  /** The same harness at the start of the day, before the warm pool and the one-hop API. */
  before: ms(runs[0].total),
};

export type ChartProvider = {
  id: string;
  label: string;
  note: string;
  date: string;
  n: number;
  successful: number;
  total: { p50: number; p95: number; p99: number };
};
// Every provider Browser Arena publishes raw results for, none left off.
export const chart = chartJson as { harness: string; providers: ChartProvider[] };
export const ours = chart.providers.find((p) => p.id === "baton")!;

/** Where our row ranks among all of them (1 = fastest), by the median, Browser Arena's own measure, and by p95. */
const place = (key: "p50" | "p95") => 1 + chart.providers.filter((p) => p.total[key] < ours.total[key]).length;
const ordinal = (n: number) => `${n}${n % 10 === 1 && n !== 11 ? "st" : n % 10 === 2 && n !== 12 ? "nd" : n % 10 === 3 && n !== 13 ? "rd" : "th"}`;
export const rank = {
  median: ordinal(place("p50")),
  p95: ordinal(place("p95")),
  of: chart.providers.length,
};

export const install = {
  npm: "npm i batonvm playwright-core",
  key: "export BATON_API_KEY=bk_…",
};

export const freeTier = [
  ["Sessions at once", "2"],
  ["Longest session", "60 minutes (15 by default)"],
  ["Browser time", "60 minutes a day"],
  ["Sign up", "Email or GitHub, no card"],
] as const;

// Only clients we have run against Baton ourselves.
export const testedWith = [
  { name: "Playwright", href: "/docs" },
  { name: "Puppeteer", href: "/docs" },
  { name: "Browser Arena", href: "/benchmarks" },
  { name: "Steel browserbench", href: "/benchmarks" },
  { name: "Any CDP client", href: "/docs" },
];

/** What every VM is refused (scripts/host/firewall.sh), and what a move returns. */
export const isolation = {
  refused: [
    "Cloud metadata",
    "The VPC and private ranges",
    "Other VMs",
    "Loopback",
    "Shared address space",
    "The node and the gateway",
    "Mail ports 25, 465, 587",
    "CDP without a signed token",
  ],
  move: {
    request: "POST /api/v1/sessions/vm-7f3a21/migrate",
    response: `{ "from": "node-a", "to": "node-b",\n  "pausedMs": ${ms(t.migration.paused.p50)}, "unreachableMs": ${ms(t.migration.unreachable.p50)} }`,
  },
  source: "/docs",
};
