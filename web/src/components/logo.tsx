import { cn } from "@/lib/cn";

/**
 * The mark: a baton passed down a line, three capsules in three layers (the construction of the
 * Kestrel site's mark). Drawn in currentColor; the lower layers are the same colour at 60% and 30%.
 */
export const MARK_PATHS = [
  { d: "M15 10h24a4 4 0 0 1 0 8h-24a4 4 0 0 1 0-8z", opacity: 1 },
  { d: "M9 19.5h24a4 4 0 0 1 0 8h-24a4 4 0 0 1 0-8z", opacity: 0.6 },
  { d: "M3 29h24a4 4 0 0 1 0 8h-24a4 4 0 0 1 0-8z", opacity: 0.3 },
] as const;

/** The mark's own box. */
export const MARK_VIEWBOX = "0 9 44 30";

export function LogoMark({ className }: { className?: string }) {
  return (
    <svg
      viewBox={MARK_VIEWBOX}
      aria-hidden="true"
      className={cn("h-5 w-auto shrink-0 text-brand", className)}
      fill="currentColor"
    >
      {MARK_PATHS.map((p) => (
        <path key={p.d} d={p.d} opacity={p.opacity} />
      ))}
    </svg>
  );
}

export function Logo({ className }: { className?: string }) {
  return (
    <span className={cn("inline-flex items-center gap-2", className)}>
      <LogoMark />
      <span className="text-[1.15rem] font-semibold tracking-[-0.03em]">
        baton
      </span>
    </span>
  );
}
