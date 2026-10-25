import { freeTier, install } from "@/content/landing";
import { CopyButton } from "@/components/site/copy-button";
import { MoreLink } from "@/components/site/frame";
import { GetKey } from "@/components/site/get-key";

/** Start hosted: what the free tier gives, the key, and the one line to set. */
export function FreeTierCard() {
  return (
    <div
      id="start"
      className="flex h-full scroll-mt-20 flex-col rounded-lg bg-card shadow-panel"
    >
      <div className="flex-1 px-5 pt-5 sm:px-6 sm:pt-6">
        <h3 className="text-[18px] font-medium tracking-[-0.015em]">
          Free to start
        </h3>
        <p className="mt-1.5 text-[14px] leading-[1.55] text-muted-foreground">
          Make an account with your email or GitHub, and the dashboard gives
          you an API key, your sessions and every event they logged.
        </p>
        <dl className="mt-5 grid grid-cols-2 gap-x-4 gap-y-3 border-t border-rule pt-4 text-[13px]">
          {freeTier.map(([k, v]) => (
            <div key={k}>
              <dt className="text-subtle">{k}</dt>
              <dd className="mt-0.5 text-foreground">{v}</dd>
            </div>
          ))}
        </dl>
        <div className="mt-5 flex items-start gap-2 rounded-md bg-muted px-3 py-2.5">
          <code className="min-w-0 flex-1 font-mono text-[12px] leading-[1.6] break-all text-foreground">
            <span className="text-subtle select-none">$ </span>
            {install.key}
          </code>
          <CopyButton value={install.key} className="-my-1" />
        </div>
        <div className="mt-5 pb-5 sm:pb-6">
          <GetKey size="md" />
        </div>
      </div>
      <div className="flex flex-wrap gap-x-6 gap-y-2 border-t border-rule px-5 py-3.5 sm:px-6">
        <MoreLink href="/docs" className="text-[13px]">
          Limits and errors
        </MoreLink>
        <MoreLink href="/status" className="text-[13px]">
          Live capacity
        </MoreLink>
      </div>
    </div>
  );
}
