import type { ReactNode } from "react";
import { DotField } from "@/components/fx/dot-field";
import { ProximityText } from "@/components/fx/proximity-text";
import { cn } from "@/lib/cn";

/** The page's column: 1200px of content with a 20px gutter (32px from sm). */
export function Frame({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn("mx-auto w-full max-w-[1264px] px-5 sm:px-8", className)}
    >
      {children}
    </div>
  );
}

/**
 * A page section: the claim on the left, a short paragraph and a link on the right, then the
 * content. Section text stays still; only the visuals move.
 */
export function Section({
  id,
  title,
  lede,
  action,
  children,
  className,
}: {
  id: string;
  title: ReactNode;
  lede?: ReactNode;
  action?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section
      id={id}
      aria-labelledby={`${id}-title`}
      className={cn(
        "relative isolate scroll-mt-16 pt-20 pb-4 sm:pt-28 sm:pb-6",
        className,
      )}
    >
      <div
        aria-hidden="true"
        className="absolute inset-x-0 top-0 -z-10 h-[340px] [mask-image:radial-gradient(ellipse_55%_75%_at_38%_42%,#000_20%,transparent_70%)] sm:h-[400px]"
      >
        <DotField tint="brand" />
      </div>
      <Frame>
        <header className="grid gap-5 lg:grid-cols-12 lg:items-end lg:gap-10">
          {typeof title === "string" ? (
            <ProximityText
              id={`${id}-title`}
              text={title}
              className="display-2 lg:col-span-7"
            />
          ) : (
            <h2 id={`${id}-title`} className="display-2 lg:col-span-7">
              {title}
            </h2>
          )}
          {lede || action ? (
            <div className="lg:col-span-5">
              {lede ? (
                <p className="text-[17px] leading-[1.6] text-muted-foreground">
                  {lede}
                </p>
              ) : null}
              {action ? <div className="mt-4">{action}</div> : null}
            </div>
          ) : null}
        </header>
        <div className="mt-12 sm:mt-14">{children}</div>
      </Frame>
    </section>
  );
}

/** A quiet link with an arrow, for "Read the threat model →". */
export function MoreLink({
  href,
  children,
  className,
}: {
  href: string;
  children: ReactNode;
  className?: string;
}) {
  return (
    <a
      href={href}
      className={cn(
        "group inline-flex items-center gap-1 text-[14px] font-medium text-foreground",
        className,
      )}
    >
      <span className="link-draw">{children}</span>
      <span
        aria-hidden="true"
        className="transition-transform duration-300 ease-out group-hover:translate-x-[3px]"
      >
        →
      </span>
    </a>
  );
}
