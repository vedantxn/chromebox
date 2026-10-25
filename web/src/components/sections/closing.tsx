import { DotField } from "@/components/fx/dot-field";
import { ProximityText } from "@/components/fx/proximity-text";
import { ButtonLink, GitHubMark } from "@/components/site/button";
import { Frame } from "@/components/site/frame";
import { GetKey } from "@/components/site/get-key";
import { batonRepo } from "@/lib/shared";

export function Closing() {
  return (
    <section
      aria-labelledby="closing-title"
      className="pt-20 pb-24 sm:pt-28 sm:pb-32"
    >
      <Frame>
        <div className="relative isolate overflow-hidden rounded-xl bg-card shadow-panel">
          <div
            aria-hidden="true"
            className="absolute inset-0 -z-10 opacity-35 lg:opacity-100"
          >
            <DotField spacing={10} wing={{ align: "right", height: 0.82 }} />
          </div>
          <div className="px-6 py-16 sm:px-12 sm:py-24 lg:max-w-[56%]">
            <ProximityText
              id="closing-title"
              text="Open source, all of it."
              className="display-2 max-w-[16ch]"
            />
            <p className="mt-4 max-w-[44ch] text-[17px] leading-[1.6] text-muted-foreground">
              The orchestrator, the node agent, the CDP proxy, the SDK and this
              site. MIT, with every benchmark&apos;s raw samples next to the code.
            </p>
            <div className="mt-8 flex flex-wrap gap-3">
              <GetKey inline />
              <ButtonLink href={batonRepo.url} variant="secondary" size="lg">
                <GitHubMark />
                Read the source
              </ButtonLink>
            </div>
          </div>
        </div>
      </Frame>
    </section>
  );
}
