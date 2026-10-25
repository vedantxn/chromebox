import { Gauge, Plug } from "lucide-react";
import { testedWith } from "@/content/landing";
import { Frame } from "@/components/site/frame";

const LOGOS: Record<string, string> = {
  Playwright: "/logos/playwright.svg",
  Puppeteer: "/logos/puppeteer.svg",
  "Steel browserbench": "/logos/steel.svg",
};

/** The clients and harnesses Baton has been run with: a static row, not a logo wall. */
export function TestedWith() {
  return (
    <section aria-label="Run with" className="pt-12 sm:pt-14">
      <Frame className="flex flex-col gap-4 sm:flex-row sm:items-center sm:gap-8">
        <span className="shrink-0 text-[13px] text-subtle">Run with</span>
        <ul className="flex flex-wrap items-center gap-x-7 gap-y-3">
          {testedWith.map((t) => (
            <li key={t.name}>
              <a
                href={t.href}
                className="flex items-center gap-2 text-[15px] font-medium tracking-[-0.01em] text-muted-foreground transition-colors hover:text-foreground"
              >
                {LOGOS[t.name] ? (
                  // eslint-disable-next-line @next/next/no-img-element -- a 20px brand mark; next/image adds nothing here
                  <img
                    src={LOGOS[t.name]}
                    alt=""
                    width={20}
                    height={20}
                    className="size-5 rounded-[5px] object-contain"
                  />
                ) : t.name === "Browser Arena" ? (
                  <Gauge className="size-[18px] text-subtle" strokeWidth={1.75} aria-hidden="true" />
                ) : (
                  <Plug className="size-[18px] text-subtle" strokeWidth={1.75} aria-hidden="true" />
                )}
                {t.name}
              </a>
            </li>
          ))}
        </ul>
      </Frame>
    </section>
  );
}
