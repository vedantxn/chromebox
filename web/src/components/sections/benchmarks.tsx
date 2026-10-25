import { arena, chart, ours, rank } from "@/content/landing";
import { MoreLink, Section } from "@/components/site/frame";
import { BenchChart, type BenchRow, type BenchTab } from "./bench-chart";

const LOGOS: Record<string, string | null> = {
  baton: null,
  kernel: "/logos/kernel.svg",
  "kernel-headful": "/logos/kernel.svg",
  "browser-use": "/logos/browser-use.png",
  hyperbrowser: "/logos/hyperbrowser.png",
  browserbase: "/logos/browserbase.svg",
  steel: "/logos/steel.svg",
  notte: "/logos/notte.png",
  anchorbrowser: "/logos/anchor.png",
  lightpanda: "/logos/lightpanda.png",
};

const ordinal = (n: number) => `${n}${["th", "st", "nd", "rd"][n % 100 >> 3 ^ 1 && n % 10] || "th"}`;
const short = (d: string) =>
  new Date(`${d}T12:00:00Z`).toLocaleDateString("en-US", { month: "short", day: "numeric", timeZone: "UTC" });

/** Every provider's whole sessions on Browser Arena, ours among them, with where they came from. */
export function ArenaChart() {
  // Providers with no logo of their own here get a plain initial, so the column stays even.
  const rows: BenchRow[] = chart.providers.map((p) => ({
    id: p.id,
    label: p.label,
    note:
      p.id === "baton"
        ? `our run, ${short(p.date)}`
        : `their run, ${short(p.date)}`,
    logo: LOGOS[p.id] ?? null,
    initial: p.id in LOGOS ? undefined : p.label[0],
    ...p.total,
  }));
  const tabs: BenchTab[] = [{ id: "arena", label: chart.harness, rows }];
  return (
    <BenchChart
      tabs={tabs}
      foot={`Every other row is Browser Arena’s own latest run, from the raw results it publishes; we did not re-run them. Baton’s row is our run of the same harness from ${arena.client}, ${arena.successful} of ${arena.runs} sessions completed.`}
    />
  );
}

export function Benchmarks() {
  return (
    <Section
      id="benchmarks"
      title={`A whole session in ${ours.total.p50} ms.`}
      lede={`Create, connect over CDP, load a page and release, timed end to end by Browser Arena’s own harness, 100 sessions in a row. ${ordinal(rank.place)} of ${rank.of} by p95, and down from ${arena.before.toLocaleString("en-US")} ms the morning we started on it.`}
      action={<MoreLink href="/benchmarks">How it was measured</MoreLink>}
    >
      <ArenaChart />
    </Section>
  );
}
