"use client";

import { motion } from "motion/react";
import { useState } from "react";
import { CopyButton } from "@/components/site/copy-button";
import { cn } from "@/lib/cn";

export type SdkTab = {
  id: string;
  label: string;
  file: string;
  install: string;
  code: string;
  html: string;
  output: string;
};

/** A code window: tabs along the top, the install line, the file, and what it prints. */
export function SdkTabs({ tabs }: { tabs: SdkTab[] }) {
  const [active, setActive] = useState(tabs[0]!.id);
  const tab = tabs.find((t) => t.id === active) ?? tabs[0]!;
  return (
    <div className="flex h-full flex-col overflow-hidden rounded-lg bg-card shadow-panel">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-rule px-3 py-2.5 sm:px-4">
        <div
          role="tablist"
          aria-label="Ways to use Baton"
          className="flex overflow-x-auto rounded-sm bg-muted p-0.5"
        >
          {tabs.map((t) => (
            <button
              key={t.id}
              role="tab"
              type="button"
              id={`tab-${t.id}`}
              aria-selected={t.id === active}
              aria-controls={`panel-${t.id}`}
              onClick={() => setActive(t.id)}
              className={cn(
                "relative shrink-0 rounded-xs px-3 py-1.5 text-[13px] font-medium whitespace-nowrap transition-colors",
                t.id === active
                  ? "text-foreground"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              {t.id === active ? (
                <motion.span
                  layoutId="sdk-tab"
                  className="absolute inset-0 rounded-xs bg-card shadow-sm"
                  transition={{ type: "spring", stiffness: 400, damping: 32 }}
                />
              ) : null}
              <span className="relative">{t.label}</span>
            </button>
          ))}
        </div>
        <span className="hidden font-mono text-[12px] text-subtle sm:inline">
          {tab.file}
        </span>
      </div>
      <div
        role="tabpanel"
        id={`panel-${tab.id}`}
        aria-labelledby={`tab-${tab.id}`}
        className="flex min-w-0 flex-1 flex-col"
      >
        <div className="flex items-center gap-3 border-b border-rule bg-muted/50 px-4 py-2 sm:px-5">
          <span className="font-mono text-[12px] text-subtle select-none">
            $
          </span>
          <code className="min-w-0 flex-1 truncate font-mono text-[12px] text-muted-foreground">
            {tab.install}
          </code>
          <CopyButton value={tab.install} />
        </div>
        <div className="relative flex-1">
          <div
            className="code-block min-h-[13.5rem] py-4 pr-10"
            data-numbers=""
            dangerouslySetInnerHTML={{ __html: tab.html }}
          />
          <CopyButton value={tab.code} className="absolute top-3 right-3" />
        </div>
        <div className="flex items-baseline gap-3 border-t border-rule px-4 py-3 sm:px-5">
          <span className="font-mono text-[11px] text-subtle">output</span>
          <pre className="min-w-0 overflow-x-auto font-mono text-[12px] leading-[1.7] text-muted-foreground">
            {tab.output}
          </pre>
        </div>
      </div>
    </div>
  );
}
