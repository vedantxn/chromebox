import Link from 'next/link';
import data from '@/data/benchmarks.json';

// The numbers come from one JSON file written by scripts/bench/run.mjs on a
// client in us-east-1; nothing here is typed in by hand.

type Stat = { n: number; p50: number; p95: number; p99: number; min?: number; max: number; unit?: string };
const d = data as unknown as {
  started_at: string;
  environment: Record<string, string | string[]>;
  tests: Record<string, Record<string, unknown>>;
  noisy?: { what: string; rows: { setup: string; p50: number; p95: number; p99: number; n: number }[] };
};
const t = d.tests;
const stat = (x: unknown) => x as Stat;
const ms = (v: number | null | undefined) => (v == null ? '–' : `${Math.round(v)} ms`);

function Row({ name, s }: { name: string; s: Stat }) {
  return (
    <tr className="border-t border-zinc-200 dark:border-zinc-800">
      <td className="py-2 pr-4">{name}</td>
      <td className="py-2 pr-4 tabular-nums">{ms(s.p50)}</td>
      <td className="py-2 pr-4 tabular-nums">{ms(s.p95)}</td>
      <td className="py-2 pr-4 tabular-nums">{ms(s.p99)}</td>
      <td className="py-2 pr-4 tabular-nums">{ms(s.max)}</td>
      <td className="py-2 tabular-nums text-zinc-500">{s.n}</td>
    </tr>
  );
}

function Table({ title, what, rows }: { title: string; what: string; rows: [string, Stat][] }) {
  return (
    <section className="mt-10">
      <h2 className="text-lg font-semibold">{title}</h2>
      <p className="mt-1 text-sm text-zinc-600 dark:text-zinc-400">{what}</p>
      <div className="mt-3 overflow-x-auto">
        <table className="w-full min-w-[480px] text-left text-sm">
          <thead className="text-zinc-500">
            <tr><th className="pb-2 pr-4 font-normal"></th><th className="pb-2 pr-4 font-normal">p50</th><th className="pb-2 pr-4 font-normal">p95</th><th className="pb-2 pr-4 font-normal">p99</th><th className="pb-2 pr-4 font-normal">max</th><th className="pb-2 font-normal">n</th></tr>
          </thead>
          <tbody>{rows.map(([n, s]) => <Row key={n} name={n} s={s} />)}</tbody>
        </table>
      </div>
    </section>
  );
}

function Tile({ value, label, note }: { value: string; label: string; note?: string }) {
  return (
    <div className="rounded-lg border border-zinc-200 p-4 dark:border-zinc-800">
      <p className="text-3xl font-semibold tabular-nums tracking-tight">{value}</p>
      <p className="mt-1 text-sm">{label}</p>
      {note && <p className="mt-1 text-xs text-zinc-500">{note}</p>}
    </div>
  );
}

export default function Benchmarks() {
  const fresh = stat(t.fresh), mig = t.migration as Record<string, Stat>, f1 = t.fork_1 as Record<string, Stat>,
    f3 = t.fork_3 as Record<string, Stat>, pub = t.public as Record<string, Stat> | undefined, den = t.density as Record<string, number[] | number>;
  const avg = (xs: number[] | number) => (Array.isArray(xs) && xs.length ? xs.reduce((a, b) => a + b, 0) / xs.length : 0);
  return (
    <main className="mx-auto max-w-3xl px-4 py-12 sm:py-16">
      <Link href="/" className="text-sm text-zinc-500 hover:underline">← Baton</Link>
      <h1 className="mt-2 text-3xl font-semibold tracking-tight">Benchmarks</h1>
      <p className="mt-3 text-zinc-600 dark:text-zinc-400">
        Measured {new Date(d.started_at).toUTCString().slice(5, 16)} from a client in {String(d.environment.region)}, on
        the same machines that serve this site. Raw samples: <a className="underline" href="/benchmarks.json">benchmarks.json</a>.
        Script: <code className="text-sm">scripts/bench/run.mjs</code>.
      </p>

      <div className="mt-8 grid gap-3 sm:grid-cols-3">
        <Tile value={ms(fresh.p50)} label="new browser, request → answering CDP" note={`p95 ${ms(fresh.p95)} · the public bar is 200 ms`} />
        <Tile value={ms(mig.paused.p50)} label="browser paused while it moves to another machine" note={`a live client's longest wait: p50 ${ms(mig.client_longest_wait.p50)}`} />
        <Tile value={ms(f1.parent_paused.p50)} label="paused to fork a running browser" note={`3 copies ready in ${ms(f3.all_copies_ready.p50)} (p50)`} />
      </div>

      <Table title="Fresh start" what={String(t.fresh.what)} rows={[['create', fresh]]} />
      {pub && <Table title="Public path, as a customer" what={String((t.public as { what: string }).what)}
        rows={[['create (API)', pub.create], ['connect (CDP)', pub.connect], ['first page', pub.first_page], ['total', pub.total]]} />}
      <Table title="Live migration under a running client" what={`${String(t.migration.what)}. Disconnects: ${String(t.migration.disconnects)}.`}
        rows={[['browser paused', mig.paused], ['unreachable (pause → network moved)', mig.unreachable], ["client's longest wait", mig.client_longest_wait],
          ['client round trip, normally', mig.client_normal_round_trip], ['memory still arriving after the move', mig.memory_streamed_after]]} />
      <Table title="Fork a running browser" what="A browser with a page open is copied: the copies share its cookies, storage and open pages; each gets its own address and fresh random state."
        rows={[['parent paused (1 copy)', f1.parent_paused], ['1 copy ready', f1.all_copies_ready], ['parent paused (3 copies)', f3.parent_paused], ['3 copies ready', f3.all_copies_ready]]} />

      <section className="mt-10">
        <h2 className="text-lg font-semibold">What a browser costs</h2>
        <p className="mt-1 text-sm text-zinc-600 dark:text-zinc-400">{String(t.density.what)}. Each browser has 1,024 MiB of guest memory.</p>
        <ul className="mt-3 space-y-1 text-sm">
          <li>fresh browser: <b className="tabular-nums">{Math.round(avg(den.fresh_private_mib))} MiB</b> its own, {Math.round(avg(den.fresh_pss_mib))} MiB counting its share of the snapshot</li>
          <li>fork copy: <b className="tabular-nums">{Math.round(avg(den.fork_copy_private_mib))} MiB</b> its own, {Math.round(avg(den.fork_copy_pss_mib))} MiB with its share</li>
          <li>fork image, shared by every copy on a machine: {String(den.fork_image_mib)} MiB</li>
        </ul>
      </section>

      {d.noisy && (
        <section className="mt-10">
          <h2 className="text-lg font-semibold">Noisy neighbour</h2>
          <p className="mt-1 text-sm text-zinc-600 dark:text-zinc-400">{d.noisy.what}</p>
          <div className="mt-3 overflow-x-auto">
            <table className="w-full min-w-[480px] text-left text-sm">
              <thead className="text-zinc-500"><tr><th className="pb-2 pr-4 font-normal">setup</th><th className="pb-2 pr-4 font-normal">p50</th><th className="pb-2 pr-4 font-normal">p95</th><th className="pb-2 pr-4 font-normal">p99</th><th className="pb-2 font-normal">n</th></tr></thead>
              <tbody>{d.noisy.rows.map((r) => (
                <tr key={r.setup} className="border-t border-zinc-200 dark:border-zinc-800">
                  <td className="py-2 pr-4">{r.setup}</td><td className="py-2 pr-4 tabular-nums">{r.p50} ms</td>
                  <td className="py-2 pr-4 tabular-nums">{r.p95} ms</td><td className="py-2 pr-4 tabular-nums">{r.p99} ms</td>
                  <td className="py-2 tabular-nums text-zinc-500">{r.n}</td>
                </tr>))}
              </tbody>
            </table>
          </div>
        </section>
      )}

      <section className="mt-10 text-sm text-zinc-600 dark:text-zinc-400">
        <h2 className="text-lg font-semibold text-zinc-900 dark:text-zinc-100">How, and what to keep in mind</h2>
        <ul className="mt-2 list-disc space-y-1 pl-5">
          <li>Machines: {String(d.environment.nodes)}. VMM: {String(d.environment.vmm)}. Guest: {String(d.environment.guest)}.</li>
          <li>Nested virtualization (KVM inside an EC2 VM) is slower than the bare-metal hosts sandbox providers usually run on; every number here would be lower there.</li>
          <li>The client: {String(d.environment.client)}. Internal tests talk to the orchestrator and gateway on private addresses; the public test goes through the internet, Vercel and CloudFront, as a customer would.</li>
          <li>Fresh start is the orchestrator&apos;s own time from request to Chrome answering, including placement, the node starting the VM in its jail, and the network route.</li>
          <li>Percentiles are over the samples listed; the raw samples are in the JSON. No warm pools: every browser is a new VM started from a snapshot.</li>
        </ul>
      </section>
    </main>
  );
}

export const metadata = { title: 'Baton · Benchmarks' };
