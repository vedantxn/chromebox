import { facts } from "@/content/landing";
import { MoreLink, Section } from "@/components/site/frame";
import { highlight } from "@/lib/highlight";
import { repoTree, sdk } from "@/lib/shared";
import { FreeTierCard } from "./get-started";
import { SdkTabs, type SdkTab } from "./sdk-tabs";

// Snippets from sdk/README.md; outputs are what they print, with the move's numbers from our runs.
const TABS: Omit<SdkTab, "html">[] = [
  {
    id: "ts",
    label: "Playwright",
    file: "first-session.ts",
    install: "npm i batonvm playwright-core",
    code: `import { Baton } from "batonvm";
import { chromium } from "playwright-core";

const baton = new Baton(); // reads BATON_API_KEY
const session = await baton.sessions.create();
const browser = await chromium.connectOverCDP(session.connectUrl);
const page = browser.contexts()[0].pages()[0];
await page.goto("https://example.com");

const move = await session.migrate(); // another machine, mid-script
console.log(\`\${move.from} → \${move.to}, paused \${move.pausedMs} ms\`);
console.log(await page.title()); // same page, same connection
await session.close();`,
    output: `node-a → node-b, paused ${facts.movePaused} ms\nExample Domain`,
  },
  {
    id: "pptr",
    label: "Puppeteer",
    file: "first-session.mjs",
    install: "npm i batonvm puppeteer-core",
    code: `import { Baton } from "batonvm";
import puppeteer from "puppeteer-core";

const baton = new Baton();
const session = await baton.sessions.create();
const browser = await puppeteer.connect({
  browserWSEndpoint: session.connectUrl,
});
const [page] = await browser.pages();
await page.goto("https://example.com");

const copies = await session.fork(3); // three more, same cookies
console.log(copies.sessions.map((s) => s.id));`,
    output: `[ 'vm-1c40e2', 'vm-88b0a7', 'vm-e4d913' ]`,
  },
  {
    id: "rest",
    label: "REST",
    file: "any language",
    install: "export BATON_API_KEY=bk_…",
    code: `curl -X POST https://batonvm.vercel.app/api/v1/sessions \\
  -H "Authorization: Bearer $BATON_API_KEY"

# then, with the id it returns
POST   /api/v1/sessions/:id/migrate
POST   /api/v1/sessions/:id/fork      { "count": 3 }
POST   /api/v1/sessions/:id/pause
POST   /api/v1/sessions/:id/resume
DELETE /api/v1/sessions/:id`,
    output: `{ "id": "vm-7f3a21", "status": "running", "connectUrl": "wss://…" }`,
  },
];

export async function Sdks() {
  const tabs: SdkTab[] = await Promise.all(
    TABS.map(async (t) => ({
      ...t,
      html: await highlight(t.code, t.id === "rest" ? "bash" : "ts"),
    })),
  );
  return (
    <Section
      id="sdks"
      title="Your code doesn’t change."
      lede="Only the browser does. Create a session, connect the Playwright or Puppeteer you already use, and call migrate, fork or pause on the same object."
      action={
        <div className="flex flex-wrap gap-x-6 gap-y-2">
          <MoreLink href="/docs">Quickstart</MoreLink>
          <MoreLink href={sdk.url}>batonvm on npm</MoreLink>
          <MoreLink href={repoTree("sdk")}>SDK source</MoreLink>
        </div>
      }
    >
      <div className="grid gap-5 lg:grid-cols-12">
        <div className="min-w-0 lg:col-span-7">
          <SdkTabs tabs={tabs} />
        </div>
        <div className="min-w-0 lg:col-span-5">
          <FreeTierCard />
        </div>
      </div>
    </Section>
  );
}
