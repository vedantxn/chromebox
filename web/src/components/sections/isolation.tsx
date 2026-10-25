import { X } from "lucide-react";
import { facts, isolation } from "@/content/landing";
import { MoreLink, Section } from "@/components/site/frame";
import { IsolationDiagram } from "./isolation-diagram";

export function Isolation() {
  return (
    <Section
      id="isolation"
      title="Move the machine, not the session."
      lede="Every browser is a Firecracker microVM with an address of its own. A move only changes the route to it, so your agent’s connection carries on, and every node’s firewall keeps VMs away from the cloud, the network and each other."
      action={<MoreLink href={isolation.source}>How migration works</MoreLink>}
    >
      <div className="overflow-hidden rounded-lg bg-card shadow-panel">
        <div className="border-b border-rule px-3 py-6 sm:px-8 sm:py-10">
          <IsolationDiagram pausedMs={facts.movePaused} />
        </div>
        <div className="grid lg:grid-cols-12">
          <div className="border-b border-rule p-6 sm:p-8 lg:col-span-7 lg:border-r lg:border-b-0">
            <h3 className="text-[16px] font-medium tracking-[-0.01em]">
              Refused for every VM
            </h3>
            <ul className="mt-4 grid gap-x-8 text-[14px] text-muted-foreground sm:grid-cols-2">
              {isolation.refused.map((r) => (
                <li
                  key={r}
                  className="flex items-center gap-2.5 border-t border-rule py-2.5 whitespace-nowrap"
                >
                  <X
                    className="size-3.5 shrink-0 text-brand"
                    strokeWidth={2.25}
                    aria-hidden="true"
                  />
                  {r}
                </li>
              ))}
            </ul>
          </div>
          <div className="p-6 sm:p-8 lg:col-span-5">
            <h3 className="text-[16px] font-medium tracking-[-0.01em]">
              What a move looks like
            </h3>
            <pre className="mt-4 overflow-x-auto rounded-md bg-muted px-4 py-3.5 font-mono text-[12px] leading-[1.75]">
              <span className="text-subtle">{"> "}</span>
              {isolation.move.request}
              {"\n"}
              <span className="text-muted-foreground">{isolation.move.response}</span>
            </pre>
            <p className="mt-3 text-[13px] text-subtle">
              Measured over {facts.moves} moves under a live client:{" "}
              {facts.disconnects} disconnects.
            </p>
          </div>
        </div>
      </div>
    </Section>
  );
}
