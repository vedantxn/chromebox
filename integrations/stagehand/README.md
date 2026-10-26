# Stagehand on Baton

[Stagehand](https://github.com/browserbase/stagehand) 3 drives any Chrome over CDP, so a Baton session drops in where a local browser would be:

```js
const session = await baton.sessions.create();
const stagehand = new Stagehand({ env: 'LOCAL', localBrowserLaunchOptions: { cdpUrl: session.connectUrl }, model: 'openai/gpt-5-mini' });
await stagehand.init();
```

[index.mjs](index.mjs) opens Hacker News, uses `act` and `extract`, then live-migrates the browser to another machine mid-task and keeps going on the same connection.

```bash
npm install
BATON_API_KEY=bk_... OPENAI_API_KEY=sk-... npm start
```

- Any model Stagehand supports works: set `model` and that provider's key.
- End with `session.close()`. `stagehand.close()` only disconnects; the remote browser would run until its timeout.
- Stagehand 4 is not supported yet: it runs inside a Chrome extension that has to be installed in the browser, and Baton's browsers don't carry it. Use `@browserbasehq/stagehand@3` (the `v3-latest` tag).

Checked against Baton with Stagehand 3.7.3: connecting, navigating, a migration mid-task, and `act` reaching the model.
