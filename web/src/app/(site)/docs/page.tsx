import Link from "next/link";
import { freeTier } from "@/content/landing";
import { MoreLink, Section, Frame } from "@/components/site/frame";
import { GetKey } from "@/components/site/get-key";
import { CodeBlock } from "@/components/site/code-block";
import { DataTable, Note, Panel, PageHead, Stat, StatRow } from "@/components/site/page";
import { pysdk, sdk } from "@/lib/shared";

export const metadata = { title: "Quickstart" };

const install = `npm install batonvm playwright-core
export BATON_API_KEY=bk_...`;

const quickstart = `import { Baton } from "batonvm";
import { chromium } from "playwright-core";

const baton = new Baton(); // reads BATON_API_KEY
const session = await baton.sessions.create();
const browser = await chromium.connectOverCDP(session.connectUrl);
const context = browser.contexts()[0];
const page = context.pages()[0] ?? (await context.newPage());

await page.goto("https://example.com");
console.log(await page.title());

const move = await session.migrate(); // the browser changes machines mid-script
console.log(\`moved \${move.from} → \${move.to}, paused \${move.pausedMs} ms\`);
console.log(await page.title()); // same page, same connection

const { sessions: [copy] } = await session.fork(1); // a copy: same pages, cookies, storage
const twin = await chromium.connectOverCDP(copy.connectUrl);
console.log(twin.contexts()[0].pages().map((p) => p.url()));
await copy.close();

// Sleep: store the browser and free its machine, then bring it back anywhere.
await session.pause();   // idle browsers cost nothing; kept up to 7 days
await session.resume();  // same pages and cookies; session.connectUrl is fresh

// Log in once, save it, and start later sessions already logged in.
await session.saveProfile("shop");
const later = await baton.sessions.create({ profile: "shop" });

await session.close();
await later.close();`;

const curl = `curl -X POST https://batonvm.vercel.app/api/v1/sessions \\
  -H "Authorization: Bearer $BATON_API_KEY"
# → { "id": "vm-...", "status": "running", "connectUrl": "wss://.../cdp/...", "expiresAt": "..." }`;

const pyInstall = `pip install batonvm playwright
export BATON_API_KEY=bk_...`;

// sdk-python/examples/: the same calls, in Python's naming (run against production before it was published).
const pyQuickstart = `from batonvm import Baton
from playwright.sync_api import sync_playwright

baton = Baton()  # reads BATON_API_KEY

with baton.sessions.create() as session, sync_playwright() as p:
    browser = p.chromium.connect_over_cdp(session.connect_url)
    page = browser.contexts[0].pages[0]
    page.goto("https://example.com")
    print(page.title())

    move = session.migrate()  # the browser changes machines mid-script
    print(f"moved {move.from_node} → {move.to_node}, paused {move.paused_ms} ms")
    print(page.title())  # same page, same connection

    copy = session.fork(1).sessions[0]  # a copy: same pages, cookies, storage
    copy.close()

    session.pause()   # stored; no machine holds it; kept up to 7 days
    session.resume()  # back on any machine; session.connect_url is fresh

    session.save_profile("shop")                     # log in once...
    later = baton.sessions.create(profile="shop")    # ...start already logged in
    later.close()
# the session ends with the with-block`;

// Run against production from us-east-1 before it went on this page.
const options = `const session = await baton.sessions.create({
  viewport: { width: 1280, height: 800 },
  userAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) ...",
  locale: "fr-FR",            // Accept-Language, navigator.language, Intl
  timezone: "Europe/Paris",   // Date and Intl in every page
  timeoutMinutes: 45,         // up to 60; the default is 15
  proxy: { server: "proxy.example.com:8080", username: "me", password: "..." },
});`;

const optionRows: [string, string][] = [
  ["viewport", "Page size in CSS pixels, 320-3840 by 240-2160."],
  ["userAgent", "Sent with every request and reported by navigator.userAgent."],
  ["locale", "A language tag like fr-FR: the Accept-Language header, navigator.language and number and date formats."],
  ["timezone", "An IANA name like Europe/Paris, for Date and Intl."],
  ["timeoutMinutes", "How long the session may run, up to 60. The default is 15."],
  ["proxy", "Your own HTTP proxy (it must support CONNECT). Every connection the browser opens goes out through it, so sites see its address. UDP is blocked, so QUIC and WebRTC can't go around it."],
];

const routes: [string, string][] = [
  ["POST /api/v1/sessions {profile?, ...options}", "Start a browser (from a saved profile: already logged in), with the options below; returns connectUrl"],
  ["GET /api/v1/sessions[/:id]", "Your sessions"],
  ["DELETE /api/v1/sessions/:id", "End it"],
  ["POST /api/v1/sessions/:id/migrate", "Move it to another machine, connection kept"],
  ["POST /api/v1/sessions/:id/fork {count}", "Copy the running browser into new sessions: same pages, cookies, storage"],
  ["POST /api/v1/sessions/:id/pause", "Store the running browser and free its machine; open connections close"],
  ["POST /api/v1/sessions/:id/resume", "Bring a paused browser back, on any machine; returns a fresh connectUrl"],
  ["GET /api/v1/sessions/:id/events", "What happened to it: created, migrated (with the pause), forked, paused, ended and why"],
  ["GET /api/v1/profiles", "Your saved profiles"],
  ["POST /api/v1/profiles {session, name}", "Save a running browser’s pages, cookies and storage; it keeps running"],
  ["DELETE /api/v1/profiles/:name", "Delete a profile"],
  ["POST /api/v1/sessions/:id/live", "A short-lived URL to watch it"],
  ["GET /api/v1/usage", "Sessions running, minutes used today"],
];

const steps = [
  {
    title: "Get a key",
    body: (
      <>
        <Link href="/sign-up">Make an account</Link> with your email or GitHub. The dashboard makes API keys, shows each
        one once, and lists your sessions with everything that happened to them.
      </>
    ),
  },
  { title: "Install the SDK and Playwright", body: <>Puppeteer works too, with <code>puppeteer.connect({"{ browserWSEndpoint: session.connectUrl }"})</code>.</> },
  {
    title: "Run a browser",
    body: <>Create a session, connect over CDP, then move it, copy it, put it to sleep and start one already logged in.</>,
  },
  {
    title: "Or from Python",
    body: (
      <>
        The same calls in Python&apos;s naming, with Playwright for Python. <code>AsyncBaton</code> is the asyncio version,
        for browser-use and Playwright&apos;s async API.
      </>
    ),
  },
];

export default function Docs() {
  return (
    <>
      <PageHead
        title="Quickstart"
        lede={
          <p>
            Sign in, set a key, and connect the Playwright you already use to a Chrome in its own
            microVM. Everything the SDK does is one REST call underneath.
          </p>
        }
      >
        <div className="flex flex-wrap items-center gap-3">
          <GetKey />
          <MoreLink href={sdk.url}>{sdk.name} on npm</MoreLink>
          <MoreLink href={pysdk.url}>{pysdk.name} on PyPI</MoreLink>
        </div>
      </PageHead>

      <Frame className="mt-16 sm:mt-20">
        <ol className="space-y-12">
          {steps.map((s, i) => (
            <li key={s.title} className="grid gap-5 lg:grid-cols-12 lg:gap-10">
              <div className="lg:col-span-4">
                <p className="label-mono text-brand-ink">{String(i + 1).padStart(2, "0")}</p>
                <h2 className="mt-2 text-[20px] font-medium tracking-[-0.015em]">{s.title}</h2>
                <Note className="mt-2 max-w-[40ch]">{s.body}</Note>
              </div>
              <div className="min-w-0 lg:col-span-8">
                {i === 0 ? (
                  <Panel className="p-5 sm:p-6">
                    <dl className="grid grid-cols-2 gap-x-4 gap-y-3 text-[13px] sm:grid-cols-4">
                      {freeTier.map(([k, v]) => (
                        <div key={k}>
                          <dt className="text-subtle">{k}</dt>
                          <dd className="mt-0.5 text-foreground">{v}</dd>
                        </div>
                      ))}
                    </dl>
                  </Panel>
                ) : i === 1 ? (
                  <CodeBlock code={install} lang="bash" file="terminal" numbers={false} />
                ) : i === 2 ? (
                  <CodeBlock code={quickstart} lang="ts" file="first-session.ts" />
                ) : (
                  <div className="space-y-4">
                    <CodeBlock code={pyInstall} lang="bash" file="terminal" numbers={false} />
                    <CodeBlock code={pyQuickstart} lang="python" file="first_session.py" />
                  </div>
                )}
              </div>
            </li>
          ))}
        </ol>
      </Frame>

      <Section
        id="options"
        title="Session options"
        lede="Set per session when you create it. The browser has them before you connect, on every tab it opens, and keeps them when it moves, forks, pauses and resumes."
      >
        <div className="space-y-5">
          <CodeBlock code={options} lang="ts" file="options.ts" />
          <Panel>
            <DataTable
              head={["option", "what it does"]}
              text={[0, 1]}
              minWidth={640}
              rows={optionRows.map(([a, b]) => [
                <code key="a" className="font-mono text-[12.5px] whitespace-nowrap text-foreground">{a}</code>,
                b,
              ])}
            />
          </Panel>
        </div>
      </Section>

      <Section
        id="rest"
        title="The REST API"
        lede={
          <>
            Every call takes <code className="font-mono text-[15px] text-foreground">Authorization: Bearer &lt;key&gt;</code>. The SDK is a thin
            layer over these.
          </>
        }
      >
        <div className="space-y-5">
          <CodeBlock code={curl} lang="bash" file="any language" numbers={false} />
          <Panel>
            <DataTable
              head={["call", "what it does"]}
              text={[0, 1]}
              minWidth={640}
              rows={routes.map(([a, b]) => [
                <code key="a" className="font-mono text-[12.5px] whitespace-nowrap text-foreground">{a}</code>,
                b,
              ])}
            />
          </Panel>
        </div>
      </Section>

      <Section
        id="limits"
        title="Limits on the free tier"
        lede="Over a limit, the API answers 429. Out of capacity, 503 with Retry-After; creating and resuming wait for the new machine, about a minute, instead of failing."
        className="pb-24 sm:pb-32"
      >
        <StatRow cols={4}>
          <Stat value="2" label="sessions running at once" note="10 new sessions a minute" />
          <Stat value="60 min" label="longest session" note="15 unless you set timeoutMinutes" />
          <Stat value="60 min" label="browser time a day" note="paused time doesn’t count" />
          <Stat value="5 · 3" label="paused sessions · profiles" note="paused ones kept 7 days" />
        </StatRow>
      </Section>
    </>
  );
}
