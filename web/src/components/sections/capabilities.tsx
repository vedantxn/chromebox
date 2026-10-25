import type { ReactNode } from "react";
import { arena, facts } from "@/content/landing";
import { Section } from "@/components/site/frame";
import { cn } from "@/lib/cn";
import {
  ForkVisual,
  MigrationVisual,
  PauseVisual,
  ProfilesVisual,
  SessionsVisual,
} from "./capability-visuals";

function Cell({
  title,
  body,
  href,
  textAt = "top",
  className,
  children,
}: {
  title: string;
  body: string;
  href: string;
  textAt?: "top" | "bottom";
  className?: string;
  children: ReactNode;
}) {
  const text = (
    <div className={cn("px-6 sm:px-7", textAt === "top" ? "pt-7" : "pb-7")}>
      <a href={href} className="group inline-flex">
        <h3 className="text-[18px] font-medium tracking-[-0.015em] group-hover:underline group-hover:decoration-rule-strong group-hover:underline-offset-4">
          {title}
        </h3>
      </a>
      <p className="mt-1.5 max-w-[40ch] text-[15px] leading-[1.5] text-muted-foreground">
        {body}
      </p>
    </div>
  );
  return (
    <div className={cn("flex min-w-0 flex-col", className)}>
      {textAt === "top" ? text : null}
      <div className="min-h-[260px] flex-1">{children}</div>
      {textAt === "bottom" ? text : null}
    </div>
  );
}

const also = [
  { label: "Autoscaling by live migration", href: "/docs" },
  { label: "A warm pool per machine", href: "/benchmarks" },
  { label: "Live view", href: "/docs" },
  { label: "Event log per session", href: "/docs" },
  { label: "Prometheus metrics", href: "/docs" },
  { label: "Status page", href: "/status" },
];

export function Capabilities() {
  const phases = [
    { name: "create", ms: arena.phases.create },
    { name: "connect", ms: arena.phases.connect },
    { name: "goto", ms: arena.phases.goto },
    { name: "release", ms: arena.phases.release },
  ];
  return (
    <Section
      id="capabilities"
      title="Move it, copy it, put it to sleep."
      lede="A real Chrome in its own microVM, driven over CDP. Move it to another machine mid-session, fork it into copies, pause it to storage and wake it anywhere."
    >
      <div className="overflow-hidden rounded-lg bg-card shadow-panel">
        <div className="grid border-b border-rule lg:grid-cols-3">
          <Cell
            title="Sessions over CDP"
            body="One call returns a wss:// URL for Playwright, Puppeteer or any CDP client. This is a real run, phase by phase."
            href="/docs"
            className="border-b border-rule lg:border-r lg:border-b-0 lg:border-dashed lg:border-r-rule-strong"
          >
            <SessionsVisual phases={phases} sessionId="vm-7f3a21" />
          </Cell>
          <Cell
            title="Live migration"
            body="Drain the machine under a running browser and it moves to another. Your agent keeps its connection, its page and its place."
            href="/docs"
            className="border-b border-rule lg:border-r lg:border-b-0 lg:border-dashed lg:border-r-rule-strong"
          >
            <MigrationVisual pausedMs={facts.movePaused} moves={facts.moves} />
          </Cell>
          <Cell
            title="Profiles"
            body="Save a logged-in browser as a profile. Every session started from it opens already logged in."
            href="/docs"
          >
            <ProfilesVisual />
          </Cell>
        </div>
        <div className="grid border-b border-rule lg:grid-cols-3">
          <Cell
            title="Fork a running browser"
            body="Copy one logged-in browser into several, each its own VM with the same pages, cookies and storage, for agents that work in parallel."
            href="/docs"
            textAt="bottom"
            className="border-b border-rule lg:col-span-2 lg:border-r lg:border-b-0"
          >
            <ForkVisual parentPausedMs={facts.forkPaused} copiesS={facts.fork3Ready} />
          </Cell>
          <Cell
            title="Pause and resume"
            body="Store an idle browser and free its machine; it costs nothing while it waits. Resume it on any machine, as it was."
            href="/docs"
            textAt="bottom"
          >
            <PauseVisual pauseS={facts.pause} resumeS={facts.resume} />
          </Cell>
        </div>
        <div className="flex flex-wrap items-center gap-x-2 gap-y-2 px-6 py-4 sm:px-7">
          <span className="mr-2 text-[13px] text-subtle">Also</span>
          {also.map((a) => (
            <a
              key={a.label}
              href={a.href}
              className="rounded-sm bg-muted px-2.5 py-1 text-[13px] text-muted-foreground transition-colors hover:text-foreground"
            >
              {a.label}
            </a>
          ))}
        </div>
      </div>
    </Section>
  );
}
