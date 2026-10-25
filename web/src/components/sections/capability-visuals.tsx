"use client";

import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { useState, type ReactNode } from "react";
import { cn } from "@/lib/cn";
import { useLoopClock, usePinnedTime } from "@/lib/use-loop-clock";

/**
 * The capability cells' visuals: small, real-looking pieces of the product rather than diagrams.
 * Hairline frames, mono only for code and data, rust on the one active thing. Each loops slowly
 * while on screen and holds its most explanatory frame under reduced motion.
 */

const OUT = [0.22, 1, 0.36, 1] as const;

function useClock(loop: number, still: number) {
  const reduce = useReducedMotion();
  const pinned = usePinnedTime(loop);
  return useLoopClock(loop, pinned ?? (reduce ? still : null));
}

function Panel({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "overflow-hidden rounded-md bg-card shadow-[0_0_0_1px_var(--rule-strong),0_8px_24px_-12px_rgb(40_30_20/0.18)]",
        className,
      )}
    >
      {children}
    </div>
  );
}

/* Sessions: the call, the URL it returns, and a real run's four phases. */
export function SessionsVisual({
  phases,
  sessionId,
}: {
  phases: { name: string; ms: number }[];
  sessionId: string;
}) {
  const { ref, t } = useClock(7, 5);
  const total = phases.reduce((s, p) => s + p.ms, 0);
  const CMD = "await baton.sessions.create()";
  const typed = CMD.slice(0, Math.floor(t / 0.03));
  const start = 1.2; // the trace begins once the call is typed
  const scale = 1.6 / total; // the whole run plays over 1.6 s
  const elapsed = Math.min(total, Math.max(0, Math.round((t - start) / scale)));
  const done = elapsed >= total;
  const starts = phases.map((_, k) =>
    phases.slice(0, k).reduce((sum, p) => sum + p.ms, 0),
  );
  return (
    <div
      ref={ref}
      className="flex h-full flex-col justify-end px-6 pb-7 sm:px-7"
    >
      <Panel>
        <div className="border-b border-rule px-3.5 py-2.5 font-mono text-[11.5px]">
          <span className="text-subtle">const</span> session ={" "}
          <span>{typed.replace("await ", "")}</span>
          {typed.length < CMD.length ? (
            <span className="ml-px inline-block h-3 w-px translate-y-0.5 animate-pulse bg-foreground" />
          ) : null}
        </div>
        <div className="space-y-2 px-3.5 py-3">
          {phases.map((p, k) => {
            const from = starts[k];
            const local = (t - start) / scale;
            const f = Math.max(0, Math.min(1, (local - from) / p.ms));
            return (
              <div
                key={p.name}
                className="grid grid-cols-[52px_minmax(0,1fr)_30px] items-center gap-3 text-[12px]"
              >
                <span className={f > 0 ? "text-foreground" : "text-subtle"}>
                  {p.name}
                </span>
                <span className="relative h-2 rounded-full bg-muted">
                  <span
                    className={cn(
                      "absolute inset-y-0 rounded-full",
                      p.name === "goto" ? "bg-ink-400" : "bg-ink-800",
                    )}
                    style={{
                      left: `${(from / total) * 100}%`,
                      width: `${(p.ms / total) * 100 * f}%`,
                    }}
                  />
                </span>
                <span className="tabular text-right font-mono text-[11px] text-muted-foreground">
                  {f > 0 ? Math.round(p.ms * f) : ""}
                </span>
              </div>
            );
          })}
        </div>
        <div className="flex items-center justify-between gap-3 border-t border-rule bg-muted/50 px-3.5 py-2 font-mono text-[11px]">
          <span
            className={cn(
              "truncate transition-colors",
              done ? "text-foreground" : "text-subtle",
            )}
          >
            wss://…/{sessionId.slice(0, 10)}…/cdp
          </span>
          <span
            className={cn(
              "tabular shrink-0",
              done ? "text-brand-ink" : "text-subtle",
            )}
          >
            {elapsed} ms
          </span>
        </div>
      </Panel>
    </div>
  );
}

/* Profiles: what a saved profile holds, as a storage inspector would show it. */
const STORE = [
  {
    tab: "Cookies",
    rows: [
      ["session_id", "shop.example", "30 days"],
      ["cart_id", "shop.example", "7 days"],
      ["__cf_bm", ".shop.example", "30 min"],
    ],
  },
  {
    tab: "Local storage",
    rows: [
      ["theme", '"light"', ""],
      ["recently_viewed", '["pocket-notebook"]', ""],
      ["consent", '"granted"', ""],
    ],
  },
  {
    tab: "IndexedDB",
    rows: [
      ["drafts", "2 records", ""],
      ["offline-cache", "14 records", ""],
      ["keyval-store", "3 records", ""],
    ],
  },
];

export function ProfilesVisual() {
  const { ref, t } = useClock(7.2, 0.5);
  const [picked, setPicked] = useState<number | null>(null);
  const i = picked ?? Math.floor(t / 2.4) % STORE.length;
  return (
    <div
      ref={ref}
      className="flex h-full flex-col justify-end px-6 pb-7 sm:px-7"
      onPointerLeave={() => setPicked(null)}
    >
      <Panel>
        <div className="flex items-center justify-between border-b border-rule px-3.5 py-2.5">
          <span className="text-[12.5px] font-medium">profile “shop”</span>
          <span className="flex items-center gap-1 text-[11px] text-success">
            <svg viewBox="0 0 16 16" className="size-3" fill="currentColor">
              <path d="M8 1a3 3 0 00-3 3v2H4a1 1 0 00-1 1v6a1 1 0 001 1h8a1 1 0 001-1V7a1 1 0 00-1-1h-1V4a3 3 0 00-3-3zm-1.5 5V4a1.5 1.5 0 013 0v2z" />
            </svg>
            encrypted
          </span>
        </div>
        <div className="flex gap-1 border-b border-rule px-2 pt-1.5">
          {STORE.map((s, k) => (
            <button
              key={s.tab}
              type="button"
              onPointerEnter={() => setPicked(k)}
              onFocus={() => setPicked(k)}
              className={cn(
                "relative px-1.5 pb-1.5 text-[11.5px] transition-colors",
                k === i ? "text-foreground" : "text-subtle",
              )}
            >
              {s.tab}
              {k === i ? (
                <motion.span
                  layoutId="profile-tab"
                  className="absolute inset-x-1 -bottom-px h-[1.5px] bg-brand"
                  transition={{ type: "spring", stiffness: 400, damping: 32 }}
                />
              ) : null}
            </button>
          ))}
        </div>
        <AnimatePresence mode="wait" initial={false}>
          <motion.ul
            key={i}
            className="px-3.5 py-1.5"
            initial={{ opacity: 0, y: 3 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.18 }}
          >
            {STORE[i].rows.map(([k, v, x]) => (
              <li
                key={k}
                className="grid grid-cols-[minmax(0,1fr)_minmax(0,1.1fr)_auto] gap-3 border-b border-dashed border-rule py-1.5 font-mono text-[11px] last:border-b-0"
              >
                <span className="truncate text-foreground">{k}</span>
                <span className="truncate text-muted-foreground">{v}</span>
                <span className="text-subtle">{x}</span>
              </li>
            ))}
          </motion.ul>
        </AnimatePresence>
        <div className="border-t border-rule bg-muted/50 px-3.5 py-2 text-[11.5px] text-muted-foreground">
          Every session started from it is already logged in
        </div>
      </Panel>
    </div>
  );
}

/* Live migration: two machines, the session moving between them, the pause counting. */
export function MigrationVisual({ pausedMs, moves }: { pausedMs: number; moves: number }) {
  const { ref, t } = useClock(6, 2.6);
  const on: "a" | "b" = t < 2.2 || t >= 5.2 ? "a" : "b";
  const moving = (t >= 1.9 && t < 2.6) || (t >= 4.9 && t < 5.6);
  const count = moving ? Math.min(pausedMs, Math.round(((t % 3) - 1.9) / 0.6 * pausedMs)) : pausedMs;
  return (
    <div ref={ref} className="flex h-full flex-col justify-end px-6 pb-7 sm:px-7">
      <Panel>
        <div className="flex items-center justify-between border-b border-rule px-3.5 py-2.5">
          <span className="text-[12.5px] font-medium">vm-7f3a21</span>
          <span className={cn("rounded-xs px-1.5 py-0.5 font-mono text-[10.5px] transition-colors", moving ? "bg-brand-tint text-brand-ink" : "bg-muted text-muted-foreground")}>
            {moving ? "migrating" : "running"}
          </span>
        </div>
        <div className="space-y-2 px-3.5 py-3">
          {(["a", "b"] as const).map((n) => (
            <div key={n} className="grid grid-cols-[52px_minmax(0,1fr)] items-center gap-3 text-[12px]">
              <span className={on === n ? "text-foreground" : "text-subtle"}>node-{n}</span>
              <span className="relative flex h-7 items-center rounded-sm bg-muted px-1">
                <AnimatePresence>
                  {on === n ? (
                    <motion.span
                      key={n}
                      className="rounded-xs bg-ink-800 px-2 py-1 font-mono text-[10.5px] text-background"
                      initial={{ opacity: 0, x: -10 }}
                      animate={{ opacity: 1, x: 0 }}
                      exit={{ opacity: 0, x: 10 }}
                      transition={{ duration: 0.35, ease: OUT }}
                    >
                      Chrome · 1 tab
                    </motion.span>
                  ) : null}
                </AnimatePresence>
              </span>
            </div>
          ))}
        </div>
        <div className="flex items-center justify-between gap-3 border-t border-rule bg-muted/50 px-3.5 py-2 font-mono text-[11px]">
          <span className="text-subtle">0 disconnects in {moves} moves</span>
          <span className={cn("tabular shrink-0", moving ? "text-brand-ink" : "text-muted-foreground")}>paused {count} ms</span>
        </div>
      </Panel>
    </div>
  );
}

/* Fork: one logged-in browser becomes three, each its own VM, same cookies and pages. */
const COPIES = [
  { id: "vm-1c40e2", node: "node-a" },
  { id: "vm-88b0a7", node: "node-b" },
  { id: "vm-e4d913", node: "node-b" },
];
export function ForkVisual({ parentPausedMs, copiesS }: { parentPausedMs: number; copiesS: string }) {
  const { ref, t } = useClock(8, 5.6);
  const forked = t >= 1.6;
  const shown = COPIES.filter((_, k) => t >= 2.4 + k * 0.7).length;
  return (
    <div
      ref={ref}
      className="grid h-full gap-6 px-6 pt-8 pb-2 sm:px-8 md:grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)] md:items-center"
    >
      <Panel>
        <div className="flex items-center justify-between border-b border-rule px-3.5 py-2.5">
          <span className="text-[12.5px] font-medium">vm-7f3a21</span>
          <span className="font-mono text-[10.5px] text-subtle">parent</span>
        </div>
        <div className="space-y-1.5 px-3.5 py-3 font-mono text-[11px]">
          {[
            ["page", "shop.example/account"],
            ["cookies", "session_id, cart_id"],
            ["logged in as", "agent@acme.dev"],
          ].map(([k, v]) => (
            <div key={k} className="grid grid-cols-[88px_minmax(0,1fr)] gap-3">
              <span className="text-subtle">{k}</span>
              <span className="truncate text-foreground">{v}</span>
            </div>
          ))}
        </div>
        <div className="flex items-center justify-between border-t border-rule bg-muted/50 px-3.5 py-2 font-mono text-[11px]">
          <span className="text-subtle">session.fork(3)</span>
          <span className={cn("tabular", forked ? "text-brand-ink" : "text-subtle")}>paused {parentPausedMs} ms</span>
        </div>
      </Panel>
      <div>
        <p className="mb-2.5 font-mono text-[11px] text-subtle">POST /api/v1/sessions/:id/fork {"{ count: 3 }"}</p>
        <ol className="space-y-1.5">
          {COPIES.map((c, k) => (
            <motion.li
              key={c.id}
              className="flex items-center gap-2 rounded-sm bg-card px-2.5 py-1.5 font-mono text-[11px] shadow-[0_0_0_1px_var(--rule-strong)]"
              initial={false}
              animate={{ opacity: k < shown ? 1 : 0, y: k < shown ? 0 : 4 }}
              transition={{ duration: 0.3, ease: OUT }}
            >
              <span className={cn("size-1.5 shrink-0 rounded-full", k < shown ? "bg-success" : "bg-brand")} />
              <span className="text-foreground">{c.id}</span>
              <span className="text-subtle">{c.node}</span>
              <span className="ml-auto truncate text-muted-foreground">logged in, same tabs</span>
            </motion.li>
          ))}
        </ol>
        <p className={cn("mt-3 text-[12.5px] text-muted-foreground transition-opacity duration-300", shown === COPIES.length ? "opacity-100" : "opacity-0")}>
          Three copies running in {copiesS} s, each with its own randomness.
        </p>
      </div>
    </div>
  );
}

/* Pause and resume: stored in S3, no machine holding it, then back on another machine. */
export function PauseVisual({ pauseS, resumeS }: { pauseS: string; resumeS: string }) {
  const { ref, t } = useClock(8, 4.5);
  const steps = [
    { at: 0.6, label: "session.pause()", done: `stored in ${pauseS} s` },
    { at: 2.2, label: "no machine holds it", done: "up to 7 days" },
    { at: 4.0, label: "session.resume()", done: `node-c, ${resumeS} s` },
  ];
  const upload = Math.max(0, Math.min(1, (t - 0.6) / 1.2));
  return (
    <div ref={ref} className="flex h-full flex-col justify-end px-6 pt-7 pb-7 sm:px-7">
      <div className="space-y-1.5">
        {steps.map((s) => {
          const shown = t >= s.at;
          const done = t >= s.at + 0.9;
          return (
            <motion.div
              key={s.label}
              className="flex items-center gap-2 rounded-sm bg-card px-2.5 py-1.5 font-mono text-[11px] shadow-[0_0_0_1px_var(--rule-strong)]"
              initial={false}
              animate={{ opacity: shown ? 1 : 0, y: shown ? 0 : 4 }}
              transition={{ duration: 0.3, ease: OUT }}
            >
              <span className={cn("size-1.5 shrink-0 rounded-full transition-colors", done ? "bg-success" : "bg-brand")} />
              <span className="text-foreground">{s.label}</span>
              <span className={cn("ml-auto truncate text-subtle transition-opacity duration-300", done ? "opacity-100" : "opacity-0")}>{s.done}</span>
            </motion.div>
          );
        })}
        <div className="mt-2 rounded-sm bg-muted px-2.5 py-2">
          <div className="flex justify-between font-mono text-[10.5px] text-subtle">
            <span>memory, disk, device state → S3</span>
            <span className="tabular">{Math.round(upload * 100)}%</span>
          </div>
          <span className="mt-1.5 block h-1.5 rounded-full bg-card">
            <span className="block h-1.5 rounded-full bg-ink-800" style={{ width: `${upload * 100}%` }} />
          </span>
        </div>
      </div>
    </div>
  );
}
