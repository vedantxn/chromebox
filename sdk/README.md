# batonvm

Cloud browsers for agents. Every session is a real Chrome in its own Firecracker microVM, started from a snapshot in about 200 ms. Sessions can be moved to another machine while they run; your CDP connection stays open and the page keeps its state.

## Quickstart

1. Sign in at **https://batonvm.vercel.app** with GitHub and create an API key.
2. Install:

```sh
npm install batonvm playwright-core
export BATON_API_KEY=bk_...
```

3. Run:

```js
import { Baton } from 'batonvm';
import { chromium } from 'playwright-core';

const baton = new Baton(); // reads BATON_API_KEY
const session = await baton.sessions.create();
const browser = await chromium.connectOverCDP(session.connectUrl);
const context = browser.contexts()[0];
const page = context.pages()[0] ?? (await context.newPage());

await page.goto('https://example.com');
console.log(await page.title());

const move = await session.migrate(); // the browser changes machines mid-script
console.log(`moved ${move.from} → ${move.to}, paused ${move.pausedMs} ms`);
console.log(await page.title()); // same page, same connection

await session.close();
```

Puppeteer works too: `puppeteer.connect({ browserWSEndpoint: session.connectUrl })`.

## API

| | |
|---|---|
| `new Baton({ apiKey?, baseUrl? })` | client; `apiKey` defaults to `BATON_API_KEY` |
| `baton.sessions.create()` | start a browser → `Session` with `connectUrl` |
| `baton.sessions.list()` / `get(id)` / `delete(id)` | |
| `session.migrate()` | move it to another machine → `{ from, to, pausedMs, unreachableMs }` |
| `session.fork(n)` | copy the running browser into `n` new sessions: same pages, cookies and storage, each with its own `connectUrl` → `{ pausedMs, sessions, errors }` |
| `session.liveUrl()` | short-lived URL for watching it |
| `session.close()` | end it |
| `baton.usage()` | running sessions and minutes used today |

Errors are `BatonError` with `status`: `401` bad key, `429` over a limit, `503` at capacity.

## Limits (free)

2 sessions at a time · 15 minutes per session · 60 browser-minutes per day · 10 new sessions per minute.
