import type { ReactNode } from "react";
import { DotField } from "@/components/fx/dot-field";
import { ProximityText } from "@/components/fx/proximity-text";
import { cn } from "@/lib/cn";
import { Frame } from "./frame";

/**
 * The pieces the inner pages are made of, in the landing page's language: a page head like the
 * hero's, white panels drawn with ring-shadows, mono column heads, tabular numbers.
 */

/** The top of an inner page: the title coming into focus over the dot field, then a lede. */
export function PageHead({
  title,
  lede,
  children,
}: {
  title: string;
  lede?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <section aria-labelledby="page-title" className="relative isolate pt-14 sm:pt-20">
      <div
        aria-hidden="true"
        className="absolute inset-x-0 -top-14 -z-10 h-[460px] [mask-image:radial-gradient(ellipse_70%_70%_at_55%_30%,#000_25%,transparent_72%)]"
      >
        <DotField tint="brand" />
      </div>
      <Frame>
        <ProximityText
          as="h1"
          id="page-title"
          text={title}
          reveal
          peak={800}
          radius={170}
          className="display-1 max-w-[22ch]"
        />
        {lede ? (
          <div className="mt-6 max-w-[62ch] text-[17px] leading-[1.6] text-muted-foreground sm:text-[18px]">
            {lede}
          </div>
        ) : null}
        {children ? <div className="mt-8">{children}</div> : null}
      </Frame>
    </section>
  );
}

/** A white panel. */
export function Panel({
  children,
  className,
  id,
}: {
  children: ReactNode;
  className?: string;
  id?: string;
}) {
  return (
    <div id={id} className={cn("overflow-hidden rounded-lg bg-card shadow-panel", className)}>
      {children}
    </div>
  );
}

/** A panel's top bar: what it shows, and a quiet note on the right. */
export function PanelHead({ title, note }: { title: ReactNode; note?: ReactNode }) {
  return (
    <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1 border-b border-rule px-5 py-3.5 sm:px-6">
      <h3 className="text-[15px] font-medium tracking-[-0.01em]">{title}</h3>
      {note ? <p className="text-[13px] text-subtle">{note}</p> : null}
    </div>
  );
}

/** Prose inside a panel, or under it. */
export function Note({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div
      className={cn(
        "text-[14px] leading-[1.65] text-muted-foreground [&_a]:text-foreground [&_a]:underline [&_a]:decoration-rule-strong [&_a]:underline-offset-[3px] [&_a:hover]:decoration-brand [&_code]:font-mono [&_code]:text-[12.5px] [&_code]:text-foreground",
        className,
      )}
    >
      {children}
    </div>
  );
}

export type Cell = ReactNode;

/**
 * A table: mono column heads, hairline rows, numbers right-aligned in tabular mono. The first
 * column is the row's name; `strong` marks rows to set in the brand colour (ours).
 */
export function DataTable({
  head,
  rows,
  strong = [],
  text = [0],
  minWidth = 520,
}: {
  head: string[];
  rows: Cell[][];
  strong?: number[];
  /** Columns of words rather than numbers: left-aligned, in the text face. */
  text?: number[];
  minWidth?: number;
}) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-left text-[14px]" style={{ minWidth }}>
        <thead>
          <tr>
            {head.map((h, i) => (
              <th
                key={i}
                scope="col"
                className={cn(
                  "label-mono px-5 pt-4 pb-2 font-medium text-subtle first:sm:pl-6 last:sm:pr-6",
                  !text.includes(i) && "text-right",
                )}
              >
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => {
            const ours = strong.includes(i);
            return (
              <tr key={i} className="border-t border-rule">
                {r.map((c, j) => (
                  <td
                    key={j}
                    className={cn(
                      "px-5 py-2.5 first:sm:pl-6 last:sm:pr-6",
                      !text.includes(j) && "tabular text-right font-mono text-[13px] whitespace-nowrap",
                      j > 0 && !ours && "text-muted-foreground",
                      ours && "font-semibold",
                      ours && !text.includes(j) && "text-brand-ink",
                    )}
                  >
                    {c}
                  </td>
                ))}
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

/** A figure with what it measures. */
export function Stat({
  value,
  label,
  note,
  tone = "plain",
}: {
  value: ReactNode;
  label: ReactNode;
  note?: ReactNode;
  tone?: "plain" | "brand" | "bad";
}) {
  return (
    <div className="px-5 py-5 sm:px-6">
      <p
        className={cn(
          "tabular font-display text-[34px] leading-none font-medium tracking-[-0.03em]",
          tone === "brand" && "text-brand-ink",
          tone === "bad" && "text-brand-ink",
        )}
      >
        {value}
      </p>
      <p className="mt-2.5 text-[14px] text-foreground">{label}</p>
      {note ? <p className="mt-1 text-[13px] text-subtle">{note}</p> : null}
    </div>
  );
}

/** A row of stats inside one panel, divided by hairlines. */
export function StatRow({ children, cols = 3 }: { children: ReactNode; cols?: 2 | 3 | 4 }) {
  return (
    <Panel>
      <div
        className={cn(
          "grid divide-y divide-rule sm:divide-x sm:divide-y-0",
          cols === 2 && "sm:grid-cols-2",
          cols === 3 && "sm:grid-cols-3",
          cols === 4 && "sm:grid-cols-2 sm:divide-y lg:grid-cols-4 lg:divide-y-0",
        )}
      >
        {children}
      </div>
    </Panel>
  );
}

/** A heading inside a page's flow, smaller than a section title. */
export function SubHead({ children, id }: { children: ReactNode; id?: string }) {
  return (
    <h3 id={id} className="mt-12 mb-4 scroll-mt-20 text-[18px] font-medium tracking-[-0.015em]">
      {children}
    </h3>
  );
}
