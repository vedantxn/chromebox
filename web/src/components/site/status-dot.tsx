"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { cn } from "@/lib/cn";

type State = "checking" | "up" | "down";

/** A dot that reads /api/status in the browser: green while node-a, the always-on machine, is up. */
export function StatusDot() {
  const [state, setState] = useState<State>("checking");
  useEffect(() => {
    fetch("/api/status")
      .then((r) => (r.ok ? r.json() : null))
      .then((s: { nodes?: { name: string; up: boolean }[] } | null) => {
        const first = s?.nodes?.find((n) => n.name === "node-a") ?? s?.nodes?.[0];
        setState(first?.up ? "up" : "down");
      })
      .catch(() => setState("down"));
  }, []);
  return (
    <Link
      href="/status"
      className="group flex items-center gap-2 text-[13px] text-subtle hover:text-foreground"
    >
      <span className="relative flex size-2">
        {state === "up" ? (
          <span className="absolute inset-0 animate-ping rounded-full bg-success opacity-40 motion-reduce:hidden" />
        ) : null}
        <span
          className={cn(
            "relative size-2 rounded-full",
            state === "up" ? "bg-success" : state === "down" ? "bg-brand" : "bg-ink-400",
          )}
        />
      </span>
      <span className="link-draw link-draw-quiet">
        {state === "up" ? "All systems running" : state === "down" ? "See the status page" : "Status"}
      </span>
    </Link>
  );
}
