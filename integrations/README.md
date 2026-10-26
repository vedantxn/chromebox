# Integrations

Baton sessions are Chrome over CDP, so tools that drive Chrome work on them unchanged. These were each run against Baton.

| | |
|---|---|
| [mcp/](mcp/) | `batonvm-mcp`: an MCP server, so Claude, Cursor and other assistants can browse, fork, pause and migrate a Baton browser |
| [stagehand/](stagehand/) | Stagehand 3 (TypeScript): `act`, `extract`, `observe` on a Baton session |
| [browser-use/](browser-use/) | browser-use (Python): its agent on a Baton session |

Playwright and Puppeteer need no integration: `chromium.connectOverCDP(session.connectUrl)` or `puppeteer.connect({ browserWSEndpoint: session.connectUrl })`.
