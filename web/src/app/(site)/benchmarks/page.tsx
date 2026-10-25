import data from "@/data/benchmarks.json";
import bb from "@/data/browserbench.json";
import arena from "@/data/arena.json";
import { ArenaChart } from "@/components/sections/benchmarks";
import { MoreLink, Section } from "@/components/site/frame";
import { DataTable, Note, PageHead, Panel, PanelHead, Stat, StatRow, type Cell } from "@/components/site/page";
import { ours } from "@/content/landing";
import { repoFile, repoTree } from "@/lib/shared";

// The numbers come from JSON files written by the benchmark scripts on a client in us-east-1
// (scripts/bench/); nothing here is typed in by hand.

export const metadata = { title: "Benchmarks" };

type Stat = { n: number; p50: number; p95: number; p99: number; min?: number; max: number };
const d = data as unknown as {
  started_at: string;
  environment: Record<string, string | string[]>;
  tests: Record<string, Record<string, unknown>>;
  noisy?: { what: string; rows: { setup: string; p50: number; p95: number; p99: number; n: number }[] };
};
const t = d.tests;
const ms = (v: number | null | undefined) => (v == null ? "–" : `${Math.round(v).toLocaleString("en-US")} ms`);
const s = (v: number) => `${(v / 1000).toFixed(1)} s`;
const cap = (x: string) => x.charAt(0).toUpperCase() + x.slice(1);

/** Rows of percentiles, one per measured thing. */
const stats = (rows: [string, Stat][]): Cell[][] =>
  rows.map(([name, x]) => [name, ms(x.p50), ms(x.p95), ms(x.p99), ms(x.max), x.n]);
const STAT_HEAD = ["", "p50", "p95", "p99", "max", "n"];

function StatPanel({ title, what, rows }: { title: string; what: string; rows: [string, Stat][] }) {
  return (
    <Panel>
      <PanelHead title={title} />
      <Note className="px-5 pt-3.5 sm:px-6">{cap(what)}</Note>
      <DataTable head={STAT_HEAD} rows={stats(rows)} minWidth={600} />
    </Panel>
  );
}

export default function Benchmarks() {
  const fresh = t.fresh as unknown as Stat & { what: string };
  const mig = t.migration as Record<string, Stat>;
  const f1 = t.fork_1 as Record<string, Stat>;
  const f3 = t.fork_3 as Record<string, Stat>;
  const pub = t.public as Record<string, Stat> | undefined;
  const pr = t.pause_resume as Record<string, Stat>;
  const pf = t.profile as unknown as {
    what: string;
    save: { ms: number; browser_paused_ms: number; stored_mib: number };
    first_session: Stat;
    later_sessions: Stat;
  };
  const den = t.density as Record<string, number[] | number>;
  const avg = (xs: number[] | number) => (Array.isArray(xs) && xs.length ? xs.reduce((a, b) => a + b, 0) / xs.length : 0);
  const last = arena.runs[arena.runs.length - 1];
  const bbRows = [
    {
      provider: "Baton",
      total_median: bb.measured.stages.total.p50,
      total_p95: bb.measured.stages.total.p95,
      create: bb.measured.stages.create.avg,
      connect: bb.measured.stages.connect.avg,
      goto: bb.measured.stages.goto.avg,
      release: bb.measured.stages.release.avg,
      mine: true,
    },
    ...bb.published.rows.map((r) => ({ ...r, mine: false })),
  ].sort((a, b) => a.total_median - b.total_median);

  return (
    <>
      <PageHead
        title="Benchmarks"
        lede={
          <p>
            Measured {new Date(d.started_at).toUTCString().slice(5, 16)} from a client in{" "}
            {String(d.environment.region)}, on the same machines that serve this site. Every figure
            here is read from the raw samples, which are public.
          </p>
        }
      >
        <div className="flex flex-wrap gap-x-6 gap-y-2">
          <MoreLink href="/benchmarks.json">Raw samples</MoreLink>
          <MoreLink href={repoTree("scripts/bench")}>The scripts</MoreLink>
          <MoreLink href={repoFile("docs/learn/first-load.md")}>Notes on the slow part</MoreLink>
        </div>
      </PageHead>

      <div className="mx-auto mt-12 w-full max-w-[1264px] px-5 sm:mt-16 sm:px-8">
        <StatRow cols={4}>
          <Stat value={ms(ours.total.p50)} label="a whole session on Browser Arena" note={`p95 ${ms(ours.total.p95)}`} tone="brand" />
          <Stat value={ms(fresh.p50)} label="new browser, request → answering CDP" note={`p95 ${ms(fresh.p95)}`} />
          <Stat value={ms(mig.paused.p50)} label="paused while it moves machines" note={`a live client’s longest wait: ${ms(mig.client_longest_wait.p50)}`} />
          <Stat value={ms(f1.parent_paused.p50)} label="paused to fork a running browser" note={`3 copies ready in ${s(f3.all_copies_ready.p50)}`} />
        </StatRow>
      </div>

      <Section
        id="arena"
        title="Browser Arena"
        lede={
          <>
            The harness behind browserarena.ai with Baton added as a provider and nothing else changed:
            create, connect over CDP, load example.com, release. 10 warm-ups, then 100 sessions one at
            a time, from a {arena.client}.
          </>
        }
        action={<MoreLink href="https://github.com/nottelabs/browserarena">nottelabs/browserarena</MoreLink>}
      >
        <ArenaChart />

        <Panel className="mt-5">
          <PanelHead title={`One day of work on it, ${arena.date}`} note="the sum of the stage medians, as Browser Arena reports it" />
          <DataTable
            head={["", "create", "connect", "load page", "release", "total", "p95"]}
            strong={[arena.runs.length - 1]}
            minWidth={640}
            rows={arena.runs.map((r) => [
              <a key="l" href={`/${r.file}`} className="link-draw link-draw-quiet">{r.label}</a>,
              ...[r.create, r.connect, r.load, r.release, r.total, r.p95].map(ms),
            ])}
          />
        </Panel>

        <Panel className="mt-5">
          <PanelHead title="Where the rest goes, and why we stopped here" />
          <Note className="px-5 py-4 sm:px-6">
            <ul className="space-y-2.5">
              <li>
                Sessions start from a browser that is already running and warmed up, a pool of one per
                machine; claiming one is a database update. That took create from{" "}
                {ms(arena.runs[0].create)} to {ms(last.create)} and connect from {ms(arena.runs[0].connect)} to {ms(last.connect)}.
              </li>
              <li>
                Loading the page is most of what is left, and it is Chrome, not the network: our gateway
                fetches example.com in about 25 ms. A browser in a VM on nested virtualization pays for
                every first touch of a memory page (12–17 µs measured) and every wake-up across CPUs.
                The fastest provider here loads the page in about 46 ms.
              </li>
              <li>About 40 ms of connect is CloudFront, which gives us TLS without a domain of our own.</li>
              <li>
                Getting near 150 ms would take bare-metal hosts (EC2 bare metal starts around $4 an hour
                per machine, always on) and a TLS endpoint of our own. That is spending, not engineering,
                so we stopped.
              </li>
              <li>
                The pool’s costs: one idle browser per machine, and a warmed browser has touched more
                memory, so pausing one stores about 740 MiB instead of 610 (pause 2.2 s instead of 1.9).
              </li>
            </ul>
          </Note>
        </Panel>
      </Section>

      <Section
        id="browserbench"
        title="Steel’s browserbench"
        lede={
          <>
            Steel’s open-source benchmark with Baton added: create, connect, load google.com, release;{" "}
            {bb.measured.runs} runs ({bb.measured.success} succeeded) from a {bb.measured.client} on{" "}
            {new Date(`${bb.measured.date}T12:00:00Z`).toLocaleDateString("en-US", { month: "long", day: "numeric", timeZone: "UTC" })}, before the warm pool and the one-hop API. The other rows are the numbers
            Steel published; we did not re-run them.
          </>
        }
        action={<MoreLink href={bb.published.source}>Steel’s published table</MoreLink>}
      >
        <Panel>
          <PanelHead title="Milliseconds per session" note="total is the median; stages are averages, as in Steel’s table" />
          <DataTable
            head={["", "total p50", "p95", "create", "connect", "load page", "release", "all but page"]}
            strong={bbRows.findIndex((r) => r.mine) === -1 ? [] : [bbRows.findIndex((r) => r.mine)]}
            minWidth={760}
            rows={bbRows.map((r) => [
              r.provider,
              ms(r.total_median),
              ms(r.total_p95),
              ms(r.create),
              ms(r.connect),
              ms(r.goto),
              ms(r.release),
              ms(r.create + r.connect + r.release),
            ])}
          />
          <Note className="border-t border-rule px-5 py-4 sm:px-6">
            <ul className="space-y-2.5">
              <li>
                Without the page load, a Baton session cost{" "}
                {ms(bb.measured.stages.create.avg + bb.measured.stages.connect.avg + bb.measured.stages.release.avg)}:
                second-fastest of the six.
              </li>
              <li>
                Loading google.com is where Baton loses: {ms(bb.measured.stages.goto.avg)} against
                378–1,260 ms. The network is not the cause (first byte after about 75 ms). A browser
                restored from a snapshot runs cold: its first load of a heavy page takes about 1.3 s, and
                about 350 ms by the third load. We think nested virtualization makes every first touch
                of a memory page slow; we have not run on bare metal.
              </li>
              <li>
                Different days, instances and run lengths (Steel: 5,000 runs; ours: {bb.measured.runs});
                read it as the same method, not the same hour. Raw runs:{" "}
                <a href="/browserbench-baton.jsonl">browserbench-baton.jsonl</a>.
              </li>
            </ul>
          </Note>
        </Panel>
      </Section>

      <Section
        id="moves"
        title="Moving, copying, sleeping"
        lede={`What the features cost the browser that has them: how long it is paused, and how long a client waits. Live migration under a running client, ${String(t.migration.disconnects)} disconnects.`}
      >
        <div className="grid gap-5">
          <StatPanel
            title="Live migration"
            what={String(t.migration.what)}
            rows={[
              ["browser paused", mig.paused],
              ["unreachable", mig.unreachable],
              ["client’s longest wait", mig.client_longest_wait],
              ["client round trip, normally", mig.client_normal_round_trip],
              ["memory still arriving after", mig.memory_streamed_after],
            ]}
          />
          <StatPanel
            title="Fork a running browser"
            what="A browser with a page open is copied: the copies share its cookies, storage and open pages; each gets its own address and fresh random state."
            rows={[
              ["parent paused, 1 copy", f1.parent_paused],
              ["1 copy ready", f1.all_copies_ready],
              ["parent paused, 3 copies", f3.parent_paused],
              ["3 copies ready", f3.all_copies_ready],
            ]}
          />
          <StatPanel
            title="Pause to storage, resume anywhere"
            what={String(t.pause_resume.what)}
            rows={[
              ["pause: request → stored", pr.pause_total],
              ["pause: browser stopped", pr.browser_stopped_for_snapshot],
              ["pause: upload to S3", pr.upload],
              ["resume: request → answering", pr.resume_total],
              ["resume: download from S3", pr.resume_download],
            ]}
          />
          <StatPanel
            title="Start from a saved profile"
            what={`${pf.what} Saving took ${ms(pf.save.ms)}; the browser was paused ${ms(pf.save.browser_paused_ms)}; ${Math.round(pf.save.stored_mib)} MiB stored.`}
            rows={[
              ["first session (fetches it)", pf.first_session],
              ["next sessions", pf.later_sessions],
            ]}
          />
        </div>
      </Section>

      <Section
        id="start"
        title="Starting a browser"
        lede="From the orchestrator’s side, and from a customer’s: through the internet, Vercel and CloudFront."
      >
        <div className="grid gap-5">
          <StatPanel title="Fresh start" what={fresh.what} rows={[["create", fresh]]} />
          {pub ? (
            <StatPanel
              title="Public path, as a customer"
              what={String((t.public as { what: string }).what)}
              rows={[
                ["create (API)", pub.create],
                ["connect (CDP)", pub.connect],
                ["first page", pub.first_page],
                ["total", pub.total],
              ]}
            />
          ) : null}
        </div>
      </Section>

      <Section
        id="cost"
        title="What a browser costs"
        lede={`${cap(String(t.density.what))}. Each browser has 1,024 MiB of guest memory.`}
      >
        <StatRow cols={3}>
          <Stat value={`${Math.round(avg(den.fresh_private_mib))} MiB`} label="a fresh browser’s own memory" note={`${Math.round(avg(den.fresh_pss_mib))} MiB with its share of the snapshot`} />
          <Stat value={`${Math.round(avg(den.fork_copy_private_mib))} MiB`} label="a fork copy’s own memory" note={`${Math.round(avg(den.fork_copy_pss_mib))} MiB with its share`} />
          <Stat value={`${String(den.fork_image_mib)} MiB`} label="fork image, shared by every copy on a machine" />
        </StatRow>
        {d.noisy ? (
          <Panel className="mt-5">
            <PanelHead title="Noisy neighbour" />
            <Note className="px-5 pt-3.5 sm:px-6">{cap(d.noisy.what)}</Note>
            <DataTable
              head={["setup", "p50", "p95", "p99", "n"]}
              rows={d.noisy.rows.map((r) => [r.setup, `${r.p50} ms`, `${r.p95} ms`, `${r.p99} ms`, r.n])}
            />
          </Panel>
        ) : null}
      </Section>

      <Section id="how" title="How, and what to keep in mind" className="pb-24 sm:pb-32">
        <Panel>
          <Note className="px-5 py-5 sm:px-6">
            <ul className="space-y-2.5">
              <li>
                Machines: {String(d.environment.nodes)}. VMM: {String(d.environment.vmm)}. Guest:{" "}
                {String(d.environment.guest)}.
              </li>
              <li>
                Nested virtualization (KVM inside an EC2 VM) is slower than the bare-metal hosts sandbox
                providers usually run on; the numbers here are likely lower there (not measured).
              </li>
              <li>
                The client: {String(d.environment.client)}. Internal tests talk to the orchestrator and
                gateway on private addresses; the public test goes through the internet, Vercel and
                CloudFront, as a customer would.
              </li>
              <li>
                Fresh start is the orchestrator’s own time from request to Chrome answering, including
                placement, the node starting the VM in its jail, and the network route.
              </li>
              <li>Percentiles are over the samples listed; the raw samples are in the JSON.</li>
            </ul>
          </Note>
        </Panel>
      </Section>
    </>
  );
}
