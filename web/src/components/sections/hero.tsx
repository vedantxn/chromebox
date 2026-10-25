import Link from "next/link";
import { arena, facts, install } from "@/content/landing";
import { ButtonLink } from "@/components/site/button";
import { CopyButton } from "@/components/site/copy-button";
import { Frame } from "@/components/site/frame";
import { GetKey } from "@/components/site/get-key";
import { sdk } from "@/lib/shared";
import { DotField } from "@/components/fx/dot-field";
import { ProximityText } from "@/components/fx/proximity-text";
import { HeroDemo } from "./hero-demo";

const HEADLINE = "Cloud Chrome that moves without dropping your agent.";

export function Hero() {
  return (
    <section
      aria-labelledby="hero-title"
      className="relative isolate pt-14 sm:pt-20 lg:pt-24"
    >
      <div
        aria-hidden="true"
        className="absolute inset-x-0 -top-14 -z-10 h-[760px] [mask-image:radial-gradient(ellipse_75%_70%_at_60%_30%,#000_25%,transparent_72%)]"
      >
        <DotField tint="brand" />
      </div>
      <Frame>
        <Link
          href={sdk.url}
          className="group inline-flex items-center gap-2 rounded-full bg-card py-1 pr-3 pl-1 text-[13px] shadow-sm transition-colors hover:bg-muted"
        >
          <span className="rounded-full bg-brand-tint px-2 py-0.5 font-mono text-[11px] font-medium text-brand-ink">
            {sdk.version}
          </span>
          <span className="text-muted-foreground group-hover:text-foreground">
            {sdk.name} is on npm, MIT
          </span>
          <span aria-hidden="true" className="text-subtle">
            →
          </span>
        </Link>

        <ProximityText
          as="h1"
          id="hero-title"
          text={HEADLINE}
          reveal
          peak={800}
          radius={170}
          adorn={{
            Chrome: (
              // eslint-disable-next-line @next/next/no-img-element -- a small brand mark
              <img
                src="/logos/chrome.svg"
                alt=""
                className="size-full drop-shadow-[0_4px_8px_rgb(0_0_0/0.18)]"
              />
            ),
          }}
          className="display-1 mt-7 max-w-[27ch]"
        />
        <div className="mt-6 grid gap-8 lg:grid-cols-12 lg:items-end">
          <div className="min-w-0 lg:col-span-7">
            <p className="max-w-[48ch] text-[17px] leading-[1.6] text-muted-foreground sm:text-[18px]">
              One API call returns a CDP URL for Playwright, Puppeteer or any
              agent. Every browser runs in its own Firecracker microVM, and it
              can change machines, split in three or sleep in storage while your
              agent stays connected.
            </p>
            <div className="mt-8 flex flex-wrap items-center gap-3">
              <GetKey />
              <ButtonLink href="/docs" variant="secondary" size="lg">
                Read the docs
              </ButtonLink>
            </div>
          </div>
          <div className="min-w-0 lg:col-span-5 lg:pb-1">
            <div className="rounded-md bg-card shadow-sm">
              <div className="flex items-center justify-between border-b border-rule px-3.5 py-2">
                <span className="text-[12.5px] font-medium">
                  Install the SDK
                </span>
                <CopyButton value={install.npm} />
              </div>
              <code className="block truncate px-3.5 py-3 font-mono text-[12px] text-muted-foreground">
                <span className="text-subtle select-none">$ </span>
                {install.npm}
              </code>
            </div>
          </div>
        </div>

        <div className="mt-12 sm:mt-16">
          <HeroDemo
            createMs={arena.phases.create}
            connectMs={arena.phases.connect}
            pausedMs={facts.movePaused}
            pauseS={facts.pause}
            sessionId="vm-7f3a21"
          />
        </div>
      </Frame>
    </section>
  );
}
