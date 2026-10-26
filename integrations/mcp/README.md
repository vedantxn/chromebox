# batonvm-mcp

An [MCP](https://modelcontextprotocol.io) server that gives Claude, Cursor or any MCP client a real Chrome in a [Baton](https://batonvm.vercel.app) microVM.

## Add it

Claude Code:

```bash
claude mcp add baton --env BATON_API_KEY=bk_... -- npx -y batonvm-mcp
```

Claude Desktop, Cursor and others (`claude_desktop_config.json`, `.cursor/mcp.json`):

```json
{
  "mcpServers": {
    "baton": { "command": "npx", "args": ["-y", "batonvm-mcp"], "env": { "BATON_API_KEY": "bk_..." } }
  }
}
```

Get a key at https://batonvm.vercel.app/dashboard.

## Tools

| Tool | |
|---|---|
| `create_session` | start a browser, optionally from a saved profile, with a viewport, user agent, locale, timezone, timeout or proxy |
| `navigate` | load a URL |
| `snapshot` | the page's accessibility tree: what is on it and the names to click by |
| `click`, `type`, `press_key` | act on the page with Playwright selectors, e.g. `role=button[name="Sign in"]` |
| `screenshot` | a PNG of the page |
| `migrate_session` | move the running browser to another machine; the page and connection survive |
| `fork_session` | copy the browser, logged-in state and all, into new sessions |
| `pause_session`, `resume_session` | store the browser and free its machine; bring it back later |
| `save_profile` | save the logged-in state; `create_session` with that profile starts there |
| `list_sessions`, `close_session` | |

Tools act on the last session used unless given a `sessionId`, so an assistant can work several browsers at once (a fork's copies, say). Running sessions end when the server exits; paused ones are kept for a week.

## From source

```bash
npm install && npm run build
claude mcp add baton --env BATON_API_KEY=bk_... -- node /path/to/integrations/mcp/dist/index.js
```
