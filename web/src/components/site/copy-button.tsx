"use client";

import { Check, Copy } from "lucide-react";
import { useState } from "react";
import { cn } from "@/lib/cn";

/** An icon copy button: a tick for 1.5 s once copied. */
export function CopyButton({
  value,
  className,
  label = "Copy",
}: {
  value: string;
  className?: string;
  label?: string;
}) {
  const [state, setState] = useState<"idle" | "copied" | "failed">("idle");
  return (
    <button
      type="button"
      aria-label={`${label} to clipboard`}
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(value);
          setState("copied");
        } catch {
          setState("failed");
        }
        setTimeout(() => setState("idle"), 1500);
      }}
      className={cn(
        "flex size-7 shrink-0 items-center justify-center rounded-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground",
        state === "copied" && "text-success hover:text-success",
        className,
      )}
    >
      <span aria-live="polite" className="sr-only">
        {state === "copied"
          ? "Copied"
          : state === "failed"
            ? "Copy failed"
            : ""}
      </span>
      {state === "copied" ? (
        <Check className="size-3.5" strokeWidth={2} />
      ) : (
        <Copy className="size-3.5" strokeWidth={1.75} />
      )}
    </button>
  );
}
