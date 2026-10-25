"use client";

import { animate, motion, useInView, useReducedMotion } from "motion/react";
import { useEffect, useRef, useState } from "react";
import { LogoMark } from "@/components/logo";
import { cn } from "@/lib/cn";

export type BenchRow = {
  id: string;
  label: string;
  /** A file in /public/logos, or null for Baton's own mark (or `initial`, when set). */
  logo: string | null;
  initial?: string;
  p50: number;
  p95: number;
  p99: number;
};

export type BenchTab = { id: string; label: string; rows: BenchRow[] };

// The axis stops here; a slower provider's bar runs off the end with a break mark.
const AXIS_MS = 1400;

/** A number that counts up from zero as its bar grows, the first time it comes into view. */
function CountUp({ value, delay }: { value: number; delay: number }) {
  const ref = useRef<HTMLSpanElement>(null);
  const inView = useInView(ref, { once: true, amount: 0.5 });
  const reduce = useReducedMotion();
  // The server renders the real number; the client starts from zero until the row is seen.
  const [shown, setShown] = useState(value);
  useEffect(() => {
    if (reduce) return;
    if (!inView) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- reset before the count, client only
      setShown(0);
      return;
    }
    const c = animate(0, value, {
      duration: 0.7,
      delay,
      ease: [0.22, 1, 0.36, 1],
      onUpdate: (x) => setShown(Math.round(x)),
    });
    return () => c.stop();
  }, [inView, value, delay, reduce]);
  return <span ref={ref}>{shown.toLocaleString("en-US")}</span>;
}

export function BenchChart({
  tabs,
  foot,
}: {
  tabs: BenchTab[];
  foot?: string;
}) {
  const [active, setActive] = useState(tabs[0].id);
  const tab = tabs.find((t) => t.id === active)!;
  // Bars are p95: the tail is what an agent waits on. p50 and p99 sit beside them.
  const rows = [...tab.rows].sort((a, b) => a.p95 - b.p95);
  return (
    <div className="overflow-hidden rounded-lg bg-card shadow-panel">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-rule px-4 py-3 sm:px-6">
        {tabs.length === 1 ? (
          <p className="text-[14px] font-medium">{tab.label}</p>
        ) : (
          <div
            role="tablist"
            aria-label="Benchmark"
            className="flex rounded-sm bg-muted p-0.5"
          >
            {tabs.map((t) => (
              <button
                key={t.id}
                role="tab"
                type="button"
                aria-selected={t.id === active}
                onClick={() => setActive(t.id)}
                className={cn(
                  "relative rounded-xs px-3 py-1.5 text-[13px] font-medium transition-colors",
                  t.id === active
                    ? "text-foreground"
                    : "text-muted-foreground hover:text-foreground",
                )}
              >
                {t.id === active ? (
                  <motion.span
                    layoutId="bench-tab"
                    className="absolute inset-0 rounded-xs bg-card shadow-sm"
                    transition={{ type: "spring", stiffness: 400, damping: 32 }}
                  />
                ) : null}
                <span className="relative">{t.label}</span>
              </button>
            ))}
          </div>
        )}
        <p className="text-[13px] text-subtle">
          Milliseconds per session, lower is better
        </p>
      </div>

      <div className="hidden grid-cols-[170px_minmax(0,1fr)_64px_64px] gap-4 px-6 pt-4 sm:grid">
        <span />
        <span className="font-mono text-[12.5px] font-medium text-muted-foreground">
          p95
        </span>
        <span className="text-right font-mono text-[12.5px] font-medium text-muted-foreground">
          p50
        </span>
        <span className="text-right font-mono text-[12.5px] font-medium text-muted-foreground">
          p99
        </span>
      </div>

      <ol className="px-4 pt-3 pb-5 sm:px-6" aria-label={`${tab.label}, p95`}>
        {rows.map((r, i) => {
          const ours = r.id === "baton";
          const over = r.p95 > AXIS_MS;
          const width = `${(Math.min(r.p95, AXIS_MS) / AXIS_MS) * 100}%`;
          return (
            <li
              key={`${tab.id}-${r.id}`}
              className="grid grid-cols-[minmax(0,1fr)_48px_52px] items-center gap-x-3 gap-y-2 py-2 sm:grid-cols-[170px_minmax(0,1fr)_64px_64px] sm:gap-4 sm:py-1.5"
            >
              <div className="col-span-3 flex items-center gap-2.5 sm:col-span-1">
                {r.logo ? (
                  // eslint-disable-next-line @next/next/no-img-element -- a 20px brand mark; next/image adds nothing here
                  <img
                    src={r.logo}
                    alt=""
                    width={20}
                    height={20}
                    className="size-5 shrink-0 rounded-[5px] object-contain"
                  />
                ) : r.initial ? (
                  <span className="grid size-5 shrink-0 place-items-center rounded-[5px] bg-muted font-mono text-[11px] text-muted-foreground">
                    {r.initial}
                  </span>
                ) : (
                  <span className="grid size-5 shrink-0 place-items-center">
                    <LogoMark className="h-3.5" />
                  </span>
                )}
                <div className="min-w-0">
                  <p
                    className={cn(
                      "text-[14px] leading-tight",
                      ours ? "font-semibold" : "font-medium",
                    )}
                  >
                    {r.label}
                  </p>
                </div>
              </div>
              <div className="relative h-8">
                <motion.div
                  className={cn(
                    "absolute inset-y-0 left-0 origin-left rounded-sm",
                    ours
                      ? "bg-brand"
                      : "bg-stage shadow-[inset_0_0_0_1px_var(--rule)]",
                  )}
                  style={{ width }}
                  initial={{ scaleX: 0 }}
                  whileInView={{ scaleX: 1 }}
                  viewport={{ once: true, amount: 0.5 }}
                  transition={{
                    duration: 0.7,
                    ease: [0.22, 1, 0.36, 1],
                    delay: i * 0.08,
                  }}
                />
                {over ? (
                  <span
                    aria-hidden="true"
                    className="absolute inset-y-0 right-[4%] w-2 -skew-x-12 border-x-2 border-card bg-stage"
                  />
                ) : null}
                <span
                  className={cn(
                    "tabular absolute top-1/2 -translate-y-1/2 font-mono text-[13px] whitespace-nowrap",
                    ours
                      ? "font-semibold text-brand-ink"
                      : "text-muted-foreground",
                  )}
                  style={
                    over
                      ? { right: "calc(4% + 18px)" }
                      : { left: `calc(${width} + 10px)` }
                  }
                >
                  <CountUp value={r.p95} delay={i * 0.08} />
                </span>
              </div>
              <span className="tabular text-right font-mono text-[13px] text-muted-foreground">
                <span className="mr-1 text-[10px] text-subtle sm:hidden">
                  p50
                </span>
                {r.p50.toLocaleString("en-US")}
              </span>
              <span className="tabular text-right font-mono text-[13px] text-muted-foreground">
                <span className="mr-1 text-[10px] text-subtle sm:hidden">
                  p99
                </span>
                {r.p99.toLocaleString("en-US")}
              </span>
            </li>
          );
        })}
      </ol>
      {foot ? (
        <p className="border-t border-rule px-4 py-3.5 text-[13px] leading-relaxed text-subtle sm:px-6">
          {foot}
        </p>
      ) : null}
    </div>
  );
}
