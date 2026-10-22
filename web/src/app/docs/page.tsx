import Link from 'next/link';

const install = `npm install https://batonvm.vercel.app/batonvm-0.1.0.tgz playwright-core
export BATON_API_KEY=bk_...`;

const quickstart = `import { Baton } from 'batonvm';
import { chromium } from 'playwright-core';

const baton = new Baton(); // reads BATON_API_KEY
const session = await baton.sessions.create();
const browser = await chromium.connectOverCDP(session.connectUrl);
const context = browser.contexts()[0];
const page = context.pages()[0] ?? (await context.newPage());

await page.goto('https://example.com');
console.log(await page.title());

const move = await session.migrate(); // the browser changes machines mid-script
console.log(\`moved \${move.from} → \${move.to}, paused \${move.pausedMs} ms\`);
console.log(await page.title()); // same page, same connection

await session.close();`;

const curl = `curl -X POST https://batonvm.vercel.app/api/v1/sessions \\
  -H "Authorization: Bearer $BATON_API_KEY"
# → { "id": "vm-...", "status": "running", "connectUrl": "wss://.../cdp/...", "expiresAt": "..." }`;

const Code = ({ children }: { children: string }) => (
  <pre className="mt-3 overflow-x-auto rounded-lg bg-zinc-100 p-4 text-[13px] leading-relaxed dark:bg-zinc-900">{children}</pre>
);

export default function Docs() {
  return (
    <main className="mx-auto max-w-3xl px-4 py-12">
      <Link href="/" className="text-sm text-amber-600 dark:text-amber-400">← Baton</Link>
      <h1 className="mt-3 text-2xl font-semibold">Quickstart</h1>
      <ol className="mt-6 list-decimal space-y-8 pl-5">
        <li><Link href="/dashboard" className="underline">Sign in with GitHub</Link> and create an API key.</li>
        <li>Install the SDK and Playwright, and set your key:<Code>{install}</Code></li>
        <li>Run a browser:<Code>{quickstart}</Code></li>
      </ol>
      <h2 className="mt-12 text-lg font-semibold">REST API</h2>
      <p className="mt-2 text-sm text-zinc-600 dark:text-zinc-400">Every call takes <code>Authorization: Bearer &lt;key&gt;</code>.</p>
      <Code>{curl}</Code>
      <table className="mt-4 w-full text-left text-sm">
        <tbody className="divide-y divide-zinc-200 dark:divide-zinc-800">
          {[
            ['POST /api/v1/sessions', 'start a browser; returns connectUrl'],
            ['GET /api/v1/sessions[/:id]', 'your sessions'],
            ['DELETE /api/v1/sessions/:id', 'end it'],
            ['POST /api/v1/sessions/:id/migrate', 'move it to another machine, connection kept'],
            ['POST /api/v1/sessions/:id/live', 'a short-lived URL to watch it'],
            ['GET /api/v1/usage', 'sessions running, minutes used today'],
          ].map(([a, b]) => <tr key={a}><td className="py-2 pr-4 font-mono">{a}</td><td className="py-2 text-zinc-600 dark:text-zinc-400">{b}</td></tr>)}
        </tbody>
      </table>
      <h2 className="mt-12 text-lg font-semibold">Limits</h2>
      <p className="mt-2 text-sm text-zinc-600 dark:text-zinc-400">
        2 sessions at a time · 15 minutes per session · 60 browser-minutes per day · 10 new sessions per minute.
        Over a limit: HTTP 429. Out of capacity: HTTP 503.
      </p>
    </main>
  );
}
