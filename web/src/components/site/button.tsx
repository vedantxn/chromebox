import type { ComponentProps } from "react";
import { cn } from "@/lib/cn";

type Variant = "primary" | "secondary" | "ghost";
type Size = "sm" | "md" | "lg";

const variants: Record<Variant, string> = {
  primary:
    "btn-sheen bg-brand text-brand-foreground shadow-[inset_0_1px_0_rgb(255_255_255/0.14),0_1px_2px_rgb(0_0_0/0.12)] hover:bg-brand-hover",
  secondary:
    "bg-card text-foreground shadow-sm hover:shadow-[0_0_0_1px_var(--rule-strong),0_2px_6px_-2px_rgb(0_0_0/0.12)]",
  ghost: "text-muted-foreground hover:bg-muted hover:text-foreground",
};

const sizes: Record<Size, string> = {
  sm: "h-8 px-3 text-[13px]",
  md: "h-9 px-3.5 text-[14px]",
  lg: "h-10 px-4 text-[14px]",
};

export function buttonClass(variant: Variant = "secondary", size: Size = "md") {
  return cn(
    "group/btn relative inline-flex shrink-0 items-center justify-center overflow-hidden rounded-sm font-medium whitespace-nowrap transition-[background-color,box-shadow] duration-200",
    variants[variant],
    sizes[size],
  );
}

/**
 * A link styled as a button. On hover the label rolls up and an identical copy rolls in from
 * below (no overshoot); the primary one also catches a single sheen of light.
 */
export function ButtonLink({
  variant,
  size,
  className,
  children,
  ...props
}: ComponentProps<"a"> & { variant?: Variant; size?: Size }) {
  return (
    <a className={cn(buttonClass(variant, size), className)} {...props}>
      <span className="btn-roll relative flex items-center gap-2">
        <span className="btn-roll-a flex items-center gap-2">{children}</span>
        <span
          aria-hidden="true"
          className="btn-roll-b absolute inset-0 flex items-center justify-center gap-2"
        >
          {children}
        </span>
      </span>
    </a>
  );
}

/** The same, as a button. */
export function Button({
  variant,
  size,
  className,
  children,
  ...props
}: ComponentProps<"button"> & { variant?: Variant; size?: Size }) {
  return (
    <button type="button" className={cn(buttonClass(variant, size), className)} {...props}>
      <span className="btn-roll relative flex items-center gap-2">
        <span className="btn-roll-a flex items-center gap-2">{children}</span>
        <span
          aria-hidden="true"
          className="btn-roll-b absolute inset-0 flex items-center justify-center gap-2"
        >
          {children}
        </span>
      </span>
    </button>
  );
}

/** GitHub's mark, drawn in currentColor. */
export function GitHubMark({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 16 16"
      aria-hidden="true"
      className={cn("size-4", className)}
      fill="currentColor"
    >
      <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z" />
    </svg>
  );
}
