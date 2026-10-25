import { Suspense } from "react";
import { Frame, MoreLink, Section } from "@/components/site/frame";
import { DataTable, Note, PageHead, Panel, PanelHead, Stat, StatRow } from "@/components/site/page";
import { cn } from "@/lib/cn";
import { getStats, health, type StatsNode } from "@/lib/stats";

export const metadata = { title: "Status" };

const ms = (v: number) => (v >= 1000 ? `${(v / 1000).toFixed(1)} s` : `${Math.round(v)} ms`);
const gib = (v: number) => `${(v / 1024).toFixed(1)} GiB`;
const clock = (at: string) => `${new Date(at).toUTCString().slice(17, 25)} UTC`;

const ROWS: [string, string][] = [
  ["create", "Start a browser"],
  ["migration_pause", "Live migration: browser paused"],
  ["fork_parent_pause", "Fork: browser paused"],
  ["pause", "Pause: stored"],
  ["resume", "Resume: back from storage"],
];

function nodeState(n: StatsNode) {
  if (n.state) return n.state;
  if (!n.up) return "stopped";
  return n.cordoned ? "closed to new browsers" : "running";
}

/** Green when up, rust when something is wrong, grey when it is off on purpose. */
function Dot({ ok, off = false }: { ok: boolean; off?: boolean }) {
  return (
    <span className="relative flex size-2.5 shrink-0">
      {ok ? (
        <span className="absolute inset-0 animate-ping rounded-full bg-success opacity-40 motion-reduce:hidden" />
      ) : null}
      <span className={cn("relative size-2.5 rounded-full", ok ? "bg-success" : off ? "bg-ink-400" : "bg-brand")} />
    </span>
  );
}

async function Body() {
  const s = await getStats();
  const h = health(s);
  const up = s?.nodes.filter((n) => n.up).length ?? 0;
  return (
    <>
      <Frame className="mt-10">
        <Panel>
          <div className="flex flex-wrap items-center justify-between gap-3 px-5 py-4 sm:px-6">
            <p className="flex items-center gap-3 text-[18px] font-medium tracking-[-0.015em]">
              <Dot ok={h.ok} />
              {h.label}
            </p>
            {s ? (
              <p className="text-[13px] text-subtle">
                Updated {clock(s.at)} · refreshes every 30 s ·{" "}
                <a href="/api/status" className="link-draw link-draw-quiet text-muted-foreground">
                  JSON
                </a>
              </p>
            ) : null}
          </div>
        </Panel>
      </Frame>

      {s ? (
        <>
          <Frame className="mt-5">
            <StatRow cols={3}>
              <Stat value={<>{up}<span className="text-subtle"> / {s.nodes.length}</span></>} label="machines up" />
              <Stat value={s.sessions.running ?? 0} label="browsers running" />
              <Stat value={s.sessions.paused ?? 0} label="paused, using no machine" />
            </StatRow>
          </Frame>

          <Section
            id="machines"
            title="Machines"
            lede="Each runs jailed Firecracker microVMs, one browser in each. Memory is what the browsers use of what the machine gives them."
          >
            <Panel>
              <DataTable
                head={["machine", "state", "browsers", "memory in use"]}
                text={[0, 1]}
                minWidth={560}
                rows={s.nodes.map((n) => [
                  <span key="n" className="flex items-center gap-2.5 font-mono text-[13px]">
                    <Dot ok={n.up && !n.cordoned} off={!n.up} />
                    {n.name}
                  </span>,
                  nodeState(n),
                  n.up ? n.vms : "–",
                  n.up && n.cap_mib > 0 ? (
                    <span key="m" className="inline-flex items-center gap-3">
                      <span className="inline-block h-1.5 w-28 overflow-hidden rounded-full bg-muted">
                        <span
                          className="block h-full rounded-full bg-brand"
                          style={{ width: `${Math.min(100, (100 * n.used_mib) / n.cap_mib)}%` }}
                        />
                      </span>
                      {gib(n.used_mib)} of {gib(n.cap_mib)}
                    </span>
                  ) : (
                    "–"
                  ),
                ])}
              />
            </Panel>
          </Section>

          <Section
            id="hour"
            title="The last hour"
            lede="Read from the event log of every session, as it happened."
            action={<MoreLink href="/benchmarks">Measured performance</MoreLink>}
          >
            <Panel>
              <DataTable
                head={["", "count", "p50", "p95"]}
                rows={ROWS.map(([k, label]) => {
                  const p = s.last_hour[k];
                  return [label, p?.n ?? 0, p?.n ? ms(p.p50_ms) : "–", p?.n ? ms(p.p95_ms) : "–"];
                })}
              />
              {Object.keys(s.failures_last_hour).length > 0 ? (
                <p className="border-t border-rule px-5 py-3.5 text-[14px] text-brand-ink sm:px-6">
                  Failures:{" "}
                  {Object.entries(s.failures_last_hour)
                    .map(([k, n]) => `${k.replace(/_/g, " ")} ×${n}`)
                    .join(", ")}
                </p>
              ) : null}
            </Panel>
          </Section>

          {s.autoscaler ? (
            <Section
              id="capacity"
              title="Capacity"
              lede="A machine starts when the others are nearly full. When load drops, its browsers move to the others, live and with no disconnects, and it stops."
            >
              <Panel>
                <PanelHead title="Recent changes" note="newest first" />
                <ul>
                  {(s.autoscaler_actions ?? []).length === 0 ? (
                    <li className="px-5 py-3.5 text-[14px] text-subtle sm:px-6">No changes lately.</li>
                  ) : null}
                  {[...(s.autoscaler_actions ?? [])].reverse().map((a, i) => (
                    <li
                      key={i}
                      className="grid grid-cols-[88px_minmax(0,1fr)] gap-x-4 border-t border-rule px-5 py-2.5 text-[14px] first:border-t-0 sm:grid-cols-[88px_200px_minmax(0,1fr)] sm:px-6"
                    >
                      <span className="tabular font-mono text-[12.5px] text-subtle">{clock(a.at).slice(0, 8)}</span>
                      <span>
                        {a.kind.replace(/_/g, " ")} <span className="font-mono text-[13px]">{a.node}</span>
                      </span>
                      <span className="col-start-2 text-muted-foreground sm:col-start-auto">{a.reason}</span>
                    </li>
                  ))}
                </ul>
              </Panel>
            </Section>
          ) : null}
        </>
      ) : null}
    </>
  );
}

export default function Status() {
  return (
    <>
      <PageHead
        title="Status"
        lede={<p>The machines, the browsers on them, and how long things took in the last hour, straight from the control plane.</p>}
      />
      <Suspense
        fallback={
          <Frame className="mt-10">
            <Panel className="px-5 py-4 text-[15px] text-subtle sm:px-6">Checking…</Panel>
          </Frame>
        }
      >
        <Body />
      </Suspense>
      <Frame className="pt-16 pb-24 sm:pb-32">
        <Note>
          These numbers come from the system itself: the event log of every session. Measured
          performance is on <a href="/benchmarks">benchmarks</a>.
        </Note>
      </Frame>
    </>
  );
}
