#!/usr/bin/env node
/**
 * batonvm-mcp: an MCP server that gives an assistant (Claude, Cursor, any MCP
 * client) a real Chrome in a Baton microVM. It speaks MCP over stdio and
 * drives the browser with Playwright over CDP.
 *
 * The assistant starts a session, then works on the current page: navigate,
 * read it (an accessibility snapshot, which names every element it can act
 * on), click, type, press keys, take screenshots. Baton's own operations are
 * tools too: move the browser to another machine, fork it into copies, pause
 * it to storage and resume it, save it as a profile. Several sessions can be
 * open at once; tools act on the last one used unless given a sessionId.
 *
 *   BATON_API_KEY=bk_... npx -y batonvm-mcp
 */
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js';
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js';
import { Baton, BatonError, type Session } from 'batonvm';
import { chromium, type Browser, type Page } from 'playwright-core';
import { z } from 'zod';

const baton = new Baton(); // BATON_API_KEY, BATON_BASE_URL

// A session and, once a tool needs it, the CDP connection to its browser.
type Open = { session: Session; browser?: Browser; page?: Page; paused?: boolean };
const open = new Map<string, Open>();
let current: string | undefined;

function pick(sessionId?: string): Open {
  const id = sessionId ?? current;
  const o = id && open.get(id);
  if (!o) throw new Error(id ? `no open session ${id} (see list_sessions)` : 'no session yet: call create_session first');
  current = o.session.id;
  return o;
}

// The page tools act on: the last tab the browser opened, else its first.
async function page(o: Open): Promise<Page> {
  if (o.page && !o.page.isClosed()) return o.page;
  if (!o.browser || !o.browser.isConnected()) {
    if (!o.session.connectUrl) throw new Error(`session ${o.session.id} has no connection (paused? call resume_session)`);
    o.browser = await chromium.connectOverCDP(o.session.connectUrl);
    const ctx = o.browser.contexts()[0];
    ctx.on('page', (p) => { o.page = p; }); // a link that opens a new tab moves the work there
  }
  const ctx = o.browser.contexts()[0];
  o.page = ctx.pages().at(-1) ?? (await ctx.newPage());
  return o.page;
}

// Drop the connection (it is re-made on the next page tool).
async function disconnect(o: Open) {
  const b = o.browser;
  o.browser = undefined;
  o.page = undefined;
  await b?.close().catch(() => {}); // over CDP this closes the connection, not the remote browser
}

const text = (s: string) => ({ content: [{ type: 'text' as const, text: s }] });
const json = (v: unknown) => text(JSON.stringify(v, null, 2));

// Every tool reports failure as a result the model can read, not a protocol error.
function tool<A>(fn: (a: A) => Promise<ReturnType<typeof text> | { content: unknown[] }>) {
  return async (a: A) => {
    try {
      return (await fn(a)) as { content: never[] };
    } catch (e) {
      const msg = e instanceof BatonError ? `Baton API ${e.status}: ${e.message}` : e instanceof Error ? e.message.split('\n')[0] : String(e);
      return { content: [{ type: 'text' as const, text: msg }], isError: true };
    }
  };
}

const sessionId = z.string().optional().describe('Which session; defaults to the one used last');
const selector = z
  .string()
  .describe('A Playwright selector. From a snapshot: role=button[name="Sign in"], role=link[name="Pricing"]. Also text=Next, or CSS such as input#email');

async function summary(p: Page) {
  return `${await p.title()}\n${p.url()}`;
}

const server = new McpServer({ name: 'baton', version: '0.1.0' });

server.registerTool(
  'create_session',
  {
    title: 'Start a cloud browser',
    description:
      'Start a Chrome in its own microVM and make it the current session. Optionally start from a saved profile ' +
      '(already logged in), and set the viewport, user agent, locale, timezone, a timeout (minutes, up to 60) or an HTTP proxy.',
    inputSchema: {
      profile: z.string().optional().describe('A saved profile to start from'),
      viewport: z.object({ width: z.number().int(), height: z.number().int() }).optional(),
      userAgent: z.string().optional(),
      locale: z.string().optional().describe('e.g. fr-FR'),
      timezone: z.string().optional().describe('IANA name, e.g. Europe/Paris'),
      timeoutMinutes: z.number().int().optional().describe('1-60; the default is 15'),
      proxy: z.object({ server: z.string().describe('host:port'), username: z.string().optional(), password: z.string().optional() }).optional(),
    },
  },
  tool(async (opts) => {
    const session = await baton.sessions.create(opts);
    open.set(session.id, { session });
    current = session.id;
    return json({ id: session.id, node: session.node, expiresAt: session.expiresAt, options: session.options });
  }),
);

server.registerTool(
  'list_sessions',
  { title: 'List sessions', description: 'Your recent Baton sessions, and which one is current.', inputSchema: {} },
  tool(async () => {
    const all = await baton.sessions.list();
    return json({ current, sessions: all.map((s) => ({ id: s.id, status: s.status, node: s.node, expiresAt: s.expiresAt, openHere: open.has(s.id) })) });
  }),
);

server.registerTool(
  'navigate',
  { title: 'Go to a URL', description: 'Load a URL in the current tab.', inputSchema: { url: z.string(), sessionId } },
  tool(async ({ url, sessionId: id }) => {
    const p = await page(pick(id));
    await p.goto(url, { waitUntil: 'domcontentloaded', timeout: 45_000 });
    return text(await summary(p));
  }),
);

server.registerTool(
  'snapshot',
  {
    title: 'Read the page',
    description:
      "The current tab's accessibility tree: every heading, link, button, field and text, with the roles and names to use in role= selectors.",
    inputSchema: { sessionId },
  },
  tool(async ({ sessionId: id }) => {
    const p = await page(pick(id));
    let tree = await p.locator('body').ariaSnapshot({ timeout: 15_000 });
    if (tree.length > 40_000) tree = tree.slice(0, 40_000) + '\n... (cut at 40,000 characters)';
    return text(`${await summary(p)}\n\n${tree}`);
  }),
);

server.registerTool(
  'click',
  { title: 'Click', description: 'Click an element.', inputSchema: { selector, sessionId } },
  tool(async ({ selector: sel, sessionId: id }) => {
    const p = await page(pick(id));
    await p.locator(sel).first().click({ timeout: 15_000 });
    await p.waitForLoadState('domcontentloaded').catch(() => {});
    return text(await summary(await page(pick(id)))); // a new tab, if the click opened one
  }),
);

server.registerTool(
  'type',
  {
    title: 'Type into a field',
    description: 'Replace a field’s text, and press Enter after if submit is set.',
    inputSchema: { selector, text: z.string(), submit: z.boolean().optional(), sessionId },
  },
  tool(async ({ selector: sel, text: t, submit, sessionId: id }) => {
    const p = await page(pick(id));
    const field = p.locator(sel).first();
    await field.fill(t, { timeout: 15_000 });
    if (submit) {
      await field.press('Enter');
      await p.waitForLoadState('domcontentloaded').catch(() => {});
    }
    return text(await summary(p));
  }),
);

server.registerTool(
  'press_key',
  { title: 'Press a key', description: 'Press a key or chord in the current tab, e.g. Enter, Escape, ArrowDown, Control+A.', inputSchema: { key: z.string(), sessionId } },
  tool(async ({ key, sessionId: id }) => {
    const p = await page(pick(id));
    await p.keyboard.press(key);
    return text(await summary(p));
  }),
);

server.registerTool(
  'screenshot',
  { title: 'Screenshot', description: 'A PNG of the current tab (the visible part, or the whole page).', inputSchema: { fullPage: z.boolean().optional(), sessionId } },
  tool(async ({ fullPage, sessionId: id }) => {
    const p = await page(pick(id));
    const png = await p.screenshot({ fullPage: !!fullPage, timeout: 20_000 });
    return { content: [{ type: 'image' as const, data: png.toString('base64'), mimeType: 'image/png' }, { type: 'text' as const, text: await summary(p) }] };
  }),
);

server.registerTool(
  'migrate_session',
  {
    title: 'Move to another machine',
    description: 'Live-migrate the running browser to another machine. Pages, cookies and the connection survive; it pauses for a fraction of a second.',
    inputSchema: { sessionId },
  },
  tool(async ({ sessionId: id }) => json(await pick(id).session.migrate())),
);

server.registerTool(
  'fork_session',
  {
    title: 'Fork into copies',
    description:
      'Copy the running browser into new sessions with the same pages, cookies and storage (a logged-in site stays logged in). ' +
      'The copies can be driven by passing their sessionId; the current session stays the same.',
    inputSchema: { count: z.number().int().min(1).max(5).optional(), sessionId },
  },
  tool(async ({ count, sessionId: id }) => {
    const o = pick(id);
    // A DevTools client attached while the snapshot is taken lingers in the
    // copies and stalls their navigations: let go first, reconnect after.
    await disconnect(o);
    const f = await o.session.fork(count ?? 1);
    for (const s of f.sessions) open.set(s.id, { session: s });
    return json({ pausedMs: f.pausedMs, copies: f.sessions.map((s) => s.id), errors: f.errors });
  }),
);

server.registerTool(
  'pause_session',
  {
    title: 'Pause to storage',
    description: 'Store the browser exactly as it is and free its machine; a paused session costs nothing and is kept 7 days. resume_session brings it back.',
    inputSchema: { sessionId },
  },
  tool(async ({ sessionId: id }) => {
    const o = pick(id);
    await disconnect(o);
    const r = await o.session.pause();
    o.paused = true; // kept when this server exits: that is what a pause is for
    return json(r);
  }),
);

server.registerTool(
  'resume_session',
  { title: 'Resume', description: 'Bring a paused browser back, on any machine, with the same pages and cookies.', inputSchema: { sessionId: z.string() } },
  tool(async ({ sessionId: id }) => {
    let o = open.get(id);
    if (!o) {
      o = { session: await baton.sessions.get(id) };
      open.set(id, o);
    }
    await o.session.resume();
    o.paused = false;
    current = id;
    return text(`resumed ${id}`);
  }),
);

server.registerTool(
  'save_profile',
  {
    title: 'Save as a profile',
    description: 'Save the browser’s pages, cookies and storage under a name; create_session with that profile starts already logged in. The session keeps running.',
    inputSchema: { name: z.string().describe('lowercase letters, digits and hyphens'), sessionId },
  },
  tool(async ({ name, sessionId: id }) => {
    const o = pick(id);
    await disconnect(o); // as for a fork: nothing attached when the snapshot is taken
    return json(await o.session.saveProfile(name));
  }),
);

server.registerTool(
  'close_session',
  { title: 'Close', description: 'End a session and destroy its browser.', inputSchema: { sessionId } },
  tool(async ({ sessionId: id }) => {
    const o = pick(id);
    await disconnect(o);
    await o.session.close();
    open.delete(o.session.id);
    if (current === o.session.id) current = [...open.keys()].at(-1);
    return text(`closed ${o.session.id}`);
  }),
);

await server.connect(new StdioServerTransport());

// Running sessions end with the server, rather than running until they time
// out; paused ones are kept.
let closing = false;
async function shutdown() {
  if (closing) return;
  closing = true;
  await Promise.allSettled(
    [...open.values()].filter((o) => !o.paused).map(async (o) => { await disconnect(o); await o.session.close(); }),
  );
  process.exit(0);
}
process.stdin.on('close', shutdown);
process.on('SIGINT', shutdown);
process.on('SIGTERM', shutdown);
