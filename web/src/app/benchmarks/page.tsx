import Link from 'next/link';
import data from '@/data/benchmarks.json';
import bb from '@/data/browserbench.json';
import arena from '@/data/arena.json';

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

      <section className="mt-10">
        <h2 className="text-lg font-semibold">Browser Arena&apos;s lifecycle benchmark</h2>
        <p className="mt-1 text-sm text-zinc-600 dark:text-zinc-400">
          The harness behind <a className="underline" href="https://www.browserarena.ai">browserarena.ai</a>{' '}
          (<a className="underline" href="https://github.com/nottelabs/browserarena">nottelabs/browserarena</a>, MIT) with Baton added as a provider and nothing else changed:
          create a session, connect over CDP, load example.com, release; 10 warm-ups, then 100 sessions one at a time, from a {arena.client}.
          The total is theirs: the sum of the stage medians. Raw runs: {arena.runs.map((r, i) => (
            <span key={r.file}>{i > 0 && ', '}<a className="underline" href={`/${r.file}`}>{r.label}</a></span>))}.
        </p>
        <h3 className="mt-4 text-sm font-semibold">One day of work on it, {arena.date}</h3>
        <div className="mt-2 overflow-x-auto">
          <table className="w-full min-w-[560px] text-left text-sm">
            <thead className="text-zinc-500"><tr>{['', 'create', 'connect', 'load page', 'release', 'total', 'p95'].map((h) => <th key={h} className="pb-2 pr-4 font-normal">{h}</th>)}</tr></thead>
            <tbody>{arena.runs.map((r, i) => (
              <tr key={r.label} className={`border-t border-zinc-200 dark:border-zinc-800 ${i === arena.runs.length - 1 ? 'font-semibold' : ''}`}>
                <td className="py-2 pr-4">{r.label}</td>
                {[r.create, r.connect, r.load, r.release, r.total, r.p95].map((v, j) => <td key={j} className="py-2 pr-4 tabular-nums">{ms(v)}</td>)}
              </tr>))}
            </tbody>
          </table>
        </div>
        <h3 className="mt-6 text-sm font-semibold">Beside the providers Browser Arena publishes raw results for</h3>
        <div className="mt-2 overflow-x-auto">
          <table className="w-full min-w-[560px] text-left text-sm">
            <thead className="text-zinc-500"><tr>{['', 'create', 'connect', 'load page', 'release', 'total', 'their run'].map((h) => <th key={h} className="pb-2 pr-4 font-normal">{h}</th>)}</tr></thead>
            <tbody>{[...arena.peers.map((p) => ({ ...p, mine: false })), { provider: 'Baton (measured by us)', date: arena.date, ...arena.runs[arena.runs.length - 1], mine: true }]
              .sort((a, b) => a.total - b.total).map((p) => (
                <tr key={p.provider} className={`border-t border-zinc-200 dark:border-zinc-800 ${p.mine ? 'font-semibold' : ''}`}>
                  <td className="py-2 pr-4">{p.provider}</td>
                  {[p.create, p.connect, p.load, p.release, p.total].map((v, j) => <td key={j} className="py-2 pr-4 tabular-nums">{ms(v)}</td>)}
                  <td className="py-2 text-zinc-500">{p.date}</td>
                </tr>))}
            </tbody>
          </table>
        </div>
        <p className="mt-2 text-sm text-zinc-600 dark:text-zinc-400">
          The other rows are Browser Arena&apos;s own runs (its raw files, the latest one-at-a-time run of each, computed the same way); we did not re-run them.
          Most are measured from us-east-1 as ours is; Notte from us-west.
        </p>
        <h3 className="mt-6 text-sm font-semibold">Where the rest goes, and why we stopped here</h3>
        <ul className="mt-2 list-disc space-y-1 pl-5 text-sm text-zinc-600 dark:text-zinc-400">
          <li>Sessions start from a browser that is already running and warmed up (a pool of one per machine); claiming one is a database update. That took create from 232 to about 60 ms and connect from 262 to about 100.</li>
          <li>Loading the page is most of what is left, and it is Chrome, not the network: our gateway fetches example.com in about 25 ms. A browser in a VM on nested virtualization pays for every first touch of a memory page (12-17 µs measured) and for every wake-up across CPUs. The fastest provider here loads the page in about 46 ms.</li>
          <li>About 40 ms of connect is CloudFront, which gives us TLS without a domain of our own.</li>
          <li>Getting near 150 ms would take bare-metal hosts (EC2 bare metal starts around $4 an hour, per machine, always on) and a TLS endpoint of our own. That is spending, not engineering, so we stopped.</li>
          <li>The pool&apos;s costs: one idle browser per machine, and a warmed browser has touched more memory, so pausing one stores about 740 MiB instead of 610 (pause 2.2 s instead of 1.9).</li>
        </ul>
        <p className="mt-2 text-sm text-zinc-600 dark:text-zinc-400">Notes: <code className="text-sm">docs/learn/first-load.md</code>. Runner, patch and scoring: <code className="text-sm">scripts/bench/browserarena/</code>.</p>
      </section>

      <section className="mt-10">
        <h2 className="text-lg font-semibold">Steel&apos;s remote-browser benchmark</h2>
        <p className="mt-1 text-sm text-zinc-600 dark:text-zinc-400">
          The open-source <a className="underline" href="https://github.com/steel-dev/browserbench">browserbench</a> (MIT) with Baton added as a provider and nothing else changed:
          create a session, connect over CDP, load google.com, release; one at a time, 10 warm-ups then {bb.measured.runs} measured runs
          ({bb.measured.success} succeeded). We ran Baton ourselves on {bb.measured.date} from a {bb.measured.client}, on a {bb.measured.account}.
          The other rows are the numbers <a className="underline" href={bb.published.source}>Steel published</a> from its own run; we did not re-run them.
        </p>
        <div className="mt-3 overflow-x-auto">
          <table className="w-full min-w-[560px] text-left text-sm">
            <thead className="text-zinc-500">
              <tr><th className="pb-2 pr-4 font-normal"></th><th className="pb-2 pr-4 font-normal">total p50</th><th className="pb-2 pr-4 font-normal">p95</th>
                <th className="pb-2 pr-4 font-normal">create</th><th className="pb-2 pr-4 font-normal">connect</th><th className="pb-2 pr-4 font-normal">load page</th><th className="pb-2 pr-4 font-normal">release</th><th className="pb-2 font-normal">all but page</th></tr>
            </thead>
            <tbody>{[
              { provider: 'Baton (measured by us)', total_median: bb.measured.stages.total.p50, total_p95: bb.measured.stages.total.p95, create: bb.measured.stages.create.avg, connect: bb.measured.stages.connect.avg, goto: bb.measured.stages.goto.avg, release: bb.measured.stages.release.avg, mine: true },
              ...bb.published.rows.map((r) => ({ ...r, provider: `${r.provider} (published)`, mine: false })),
            ].sort((a, b) => a.total_median - b.total_median).map((r) => (
              <tr key={r.provider} className={`border-t border-zinc-200 dark:border-zinc-800 ${r.mine ? 'font-semibold' : ''}`}>
                <td className="py-2 pr-4">{r.provider}</td>
                <td className="py-2 pr-4 tabular-nums">{ms(r.total_median)}</td><td className="py-2 pr-4 tabular-nums">{ms(r.total_p95)}</td>
                <td className="py-2 pr-4 tabular-nums">{ms(r.create)}</td><td className="py-2 pr-4 tabular-nums">{ms(r.connect)}</td>
                <td className="py-2 pr-4 tabular-nums">{ms(r.goto)}</td><td className="py-2 pr-4 tabular-nums">{ms(r.release)}</td>
                <td className="py-2 tabular-nums">{ms(r.create + r.connect + r.release)}</td>
              </tr>))}
            </tbody>
          </table>
        </div>
        <p className="mt-2 text-sm text-zinc-600 dark:text-zinc-400">
          Total is the median of create + connect + load + release (p95 beside it); the stage columns are averages, as in Steel&apos;s table. Raw runs:{' '}
          <a className="underline" href="/browserbench-baton.jsonl">browserbench-baton.jsonl</a>. Runner and patch: <code className="text-sm">scripts/bench/browserbench/</code>.
        </p>
        <ul className="mt-2 list-disc space-y-1 pl-5 text-sm text-zinc-600 dark:text-zinc-400">
          <li>Without the page load, a Baton session costs {ms(bb.measured.stages.create.avg + bb.measured.stages.connect.avg + bb.measured.stages.release.avg)}: second-fastest of the six.</li>
          <li>Loading google.com is where Baton loses: {ms(bb.measured.stages.goto.avg)} against 378–1,260 ms. The network is not the cause (first byte after about 75 ms). A browser restored from a snapshot runs cold: its first load of a heavy page takes about 1.3 s, and the same page takes about 350 ms by the third load in the same browser. We think nested virtualization makes every first touch of a memory page slow (measured: 12–17 µs per page); we have not run on bare metal.</li>
          <li>Tried and not adopted: warming Chrome before the snapshot (the gain does not survive a restore), pre-mapping guest memory read-only (slower) or writable (about 300 ms faster to load, about 570 ms slower to start). Notes: <code className="text-sm">docs/learn/first-load.md</code>.</li>
          <li>Different days, instances and run lengths (Steel: 5,000 runs; ours: {bb.measured.runs}); read it as the same method, not the same hour.</li>
        </ul>
      </section>

      <Table title="Live migration under a running client" what={`${String(t.migration.what)}. Disconnects: ${String(t.migration.disconnects)}.`}
        rows={[['browser paused', mig.paused], ['unreachable (pause → network moved)', mig.unreachable], ["client's longest wait", mig.client_longest_wait],
          ['client round trip, normally', mig.client_normal_round_trip], ['memory still arriving after the move', mig.memory_streamed_after]]} />
      <Table title="Fork a running browser" what="A browser with a page open is copied: the copies share its cookies, storage and open pages; each gets its own address and fresh random state."
        rows={[['parent paused (1 copy)', f1.parent_paused], ['1 copy ready', f1.all_copies_ready], ['parent paused (3 copies)', f3.parent_paused], ['3 copies ready', f3.all_copies_ready]]} />

      {t.pause_resume && (() => { const pr = t.pause_resume as Record<string, Stat>; return (
        <Table title="Pause to storage, resume anywhere" what={String(t.pause_resume.what)}
          rows={[['pause: request → stored and stopped', pr.pause_total], ['pause: the browser stopped for the snapshot', pr.browser_stopped_for_snapshot],
            ['pause: upload to S3', pr.upload], ['resume: request → Chrome answering', pr.resume_total], ['resume: download from S3', pr.resume_download]]} />); })()}
      {t.profile && (() => { const pf = t.profile as unknown as { what: string; save: { ms: number; browser_paused_ms: number; stored_mib: number }; first_session: Stat; later_sessions: Stat }; return (
        <>
          <Table title="Start from a saved profile (already logged in)" what={pf.what}
            rows={[['first session (fetches the profile to a machine)', pf.first_session], ['next sessions', pf.later_sessions]]} />
          <p className="mt-2 text-sm text-zinc-600 dark:text-zinc-400">Saving the profile took {Math.round(pf.save.ms)} ms; the browser was paused {Math.round(pf.save.browser_paused_ms)} ms; {Math.round(pf.save.stored_mib)} MiB stored.</p>
        </>); })()}

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
          <li>Nested virtualization (KVM inside an EC2 VM) is slower than the bare-metal hosts sandbox providers usually run on; the numbers here are likely lower there (not measured).</li>
          <li>The client: {String(d.environment.client)}. Internal tests talk to the orchestrator and gateway on private addresses; the public test goes through the internet, Vercel and CloudFront, as a customer would.</li>
          <li>Fresh start is the orchestrator&apos;s own time from request to Chrome answering, including placement, the node starting the VM in its jail, and the network route.</li>
          <li>Every browser is a new VM restored from a snapshot. Each node keeps two jailed Firecracker processes started ahead of time (no guest yet, nothing about the browser to come); a create claims one and restores into it.</li>
          <li>Percentiles are over the samples listed; the raw samples are in the JSON.</li>
        </ul>
      </section>
    </main>
  );
}

export const metadata = { title: 'Benchmarks' };
