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
| `baton.sessions.create(options?)` | start a browser → `Session` with `connectUrl`; options below |
| `baton.sessions.list()` / `get(id)` / `delete(id)` | |
| `session.migrate()` | move it to another machine → `{ from, to, pausedMs, unreachableMs }` |
| `session.pause()` / `session.resume()` | store the running browser and free its machine (idle browsers cost nothing, up to 7 days); `resume()` brings it back exactly as it was, on any machine, with a fresh `connectUrl` |
| `session.saveProfile(name)` / `baton.profiles.list()` / `delete(name)` | save a running browser's pages, cookies and storage as a profile (it keeps running); `baton.sessions.create({ profile: name })` starts a new session already logged in |
| `session.fork(n)` | copy the running browser into `n` new sessions: same pages, cookies and storage, each with its own `connectUrl` → `{ pausedMs, sessions, errors }` |
| `session.liveUrl()` | short-lived URL for watching it |
| `session.close()` | end it |
| `baton.usage()` | running sessions and minutes used today |

Errors are `BatonError` with `status`: `401` bad key, `429` over a limit, `503` at capacity. When every machine is full and a new one is starting, `sessions.create()` and `resume()` wait for it (about a minute) instead of failing.

## Session options

Set per session at create. The browser has them before you connect, on every tab, and keeps them when it migrates, forks, pauses and resumes.

```ts
const session = await baton.sessions.create({
  viewport: { width: 1280, height: 800 },     // 320-3840 by 240-2160
  userAgent: 'Mozilla/5.0 (Macintosh; ...)',
  locale: 'fr-FR',                            // Accept-Language, navigator.language, Intl
  timezone: 'Europe/Paris',                   // Date and Intl
  timeoutMinutes: 45,                         // up to 60 (default 15)
  proxy: { server: 'proxy.example.com:8080', username: 'me', password: '...' }, // HTTP with CONNECT
});
```

With a proxy, every connection the browser opens leaves through it (sites see its address), and UDP is blocked so QUIC and WebRTC can't go around it. `session.options` shows what a session asked for; a proxy password is never returned.

## Limits (free)

2 sessions running at a time · 15 minutes per session (up to 60 with `timeoutMinutes`) · 60 browser-minutes per day · 10 new sessions per minute · 5 paused sessions (kept 7 days; paused time is not browser time) · 3 profiles.
