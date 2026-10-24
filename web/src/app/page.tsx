import { auth } from '@/auth';
import SignIn from './sign-in';
import Link from 'next/link';

const quickstart = `npm install batonvm playwright-core

import { Baton } from 'batonvm';
import { chromium } from 'playwright-core';

const baton = new Baton({ apiKey: process.env.BATON_API_KEY });
const session = await baton.sessions.create();
const browser = await chromium.connectOverCDP(session.connectUrl);
const page = browser.contexts()[0].pages()[0] ?? await browser.contexts()[0].newPage();
await page.goto('https://example.com');
console.log(await page.title());
await session.close();`;

export default async function Home() {
  const session = await auth();
  return (
    <main className="mx-auto max-w-3xl px-4 py-16 sm:py-24">
      <p className="text-sm font-medium text-amber-600 dark:text-amber-400">Baton</p>
      <h1 className="mt-2 text-3xl font-semibold tracking-tight sm:text-4xl">
        Cloud browsers for agents that keep running while the machine under them changes.
      </h1>
      <p className="mt-4 text-zinc-600 dark:text-zinc-400">
        Each browser is a Firecracker microVM restored from a snapshot, ready in about 200 ms.
        When a machine is drained, the browser moves to another one mid-session: your agent&apos;s
        connection stays open and the page keeps its state.
      </p>
      <div className="mt-8 flex flex-wrap gap-3">
        {session?.uid ? (
          <Link href="/dashboard" className="rounded-md bg-zinc-900 px-4 py-2 text-sm font-medium text-white dark:bg-white dark:text-zinc-900">
            Open dashboard
          </Link>
        ) : (
          <SignIn />
        )}
        <Link href="/docs" className="rounded-md border border-zinc-300 px-4 py-2 text-sm font-medium dark:border-zinc-700">
          Quickstart
        </Link>
        <Link href="/benchmarks" className="rounded-md border border-zinc-300 px-4 py-2 text-sm font-medium dark:border-zinc-700">
          Benchmarks
        </Link>
        <Link href="/status" className="rounded-md border border-zinc-300 px-4 py-2 text-sm font-medium dark:border-zinc-700">
          Status
        </Link>
      </div>
      <pre className="mt-10 overflow-x-auto rounded-lg bg-zinc-100 p-4 text-[13px] leading-relaxed dark:bg-zinc-900">{quickstart}</pre>
      <ul className="mt-10 grid gap-4 text-sm sm:grid-cols-3">
        <li><span className="block text-2xl font-semibold">~200 ms</span>from request to a browser that answers</li>
        <li><span className="block text-2xl font-semibold">0</span>dropped connections across live migrations</li>
        <li><span className="block text-2xl font-semibold">2 · 15 min</span>free: sessions at once · minutes each</li>
      </ul>
    </main>
  );
}

// Reads the session cookie, so it is rendered per request.
export const instant = false;
