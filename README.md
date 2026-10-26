# Baton

**Cloud browsers for AI agents that can move, copy and sleep while they run.**

Every session is a real Chrome in its own Firecracker microVM, restored from a snapshot in about 175 ms. Because a browser is a VM, not a process in a container, Baton can do what most browser infrastructure can't:

- **Live migration.** A running browser moves to another machine mid-session. The agent's CDP connection stays open and its page keeps working; the browser is paused for about 250 ms.
- **Fork.** A logged-in browser becomes N browsers with the same pages, cookies and storage, so N agents continue in parallel as the same user.
- **Pause and resume.** An idle browser is stored in S3, where it costs nothing, and resumed later on any machine, exactly as it was.
- **Profiles.** Log in once; every later session starts already logged in.

Sessions also take their own viewport, user agent, locale, time zone, timeout (up to an hour) and proxy. Machines start and stop with load, and draining one migrates its browsers away, so no session ends because a machine did.

**Try it:** **[batonvm.vercel.app](https://batonvm.vercel.app)** (sign in with GitHub or email, get an API key) · [Docs](https://batonvm.vercel.app/docs) · [Benchmarks](https://batonvm.vercel.app/benchmarks) · [Status](https://batonvm.vercel.app/status)

| | |
|---|---|
| TypeScript | `npm install batonvm` ([sdk/](sdk/)) |
| Python | `pip install batonvm` ([sdk-python/](sdk-python/), sync and async) |
| MCP (Claude, Cursor) | `npx -y batonvm-mcp` ([integrations/mcp/](integrations/mcp/)) |
| Stagehand, browser-use | examples in [integrations/](integrations/) |
| Playwright, Puppeteer | connect to `session.connectUrl`, nothing to install |

```js
import { Baton } from 'batonvm';
import { chromium } from 'playwright-core';

const baton = new Baton(); // BATON_API_KEY
const session = await baton.sessions.create();
const browser = await chromium.connectOverCDP(session.connectUrl);
const page = browser.contexts()[0].pages()[0] ?? await browser.contexts()[0].newPage();
await page.goto('https://example.com');

await session.migrate();                    // the browser changes machines; `page` keeps working
const { sessions } = await session.fork(1); // a copy: same pages, cookies, storage

await session.pause();                      // stored in S3; no machine holds it
await session.resume();                     // back on any machine, exactly as it was
await session.saveProfile('shop');          // later: baton.sessions.create({ profile: 'shop' })
```

<details>
<summary>The same in Python</summary>

```python
from batonvm import Baton
from playwright.sync_api import sync_playwright

baton = Baton()  # BATON_API_KEY
with baton.sessions.create() as session, sync_playwright() as p:
    page = p.chromium.connect_over_cdp(session.connect_url).contexts[0].pages[0]
    page.goto("https://example.com")

    session.migrate()                   # the browser changes machines; `page` keeps working
    copy = session.fork(1).sessions[0]  # a copy: same pages, cookies, storage
    copy.close()
    session.pause()                     # stored in S3; no machine holds it
    session.resume()                    # back on any machine, exactly as it was
```
</details>

## What it does, measured

Measured 2026-10-26 from a client in us-east-1, on 2 × m8i.large with nested virtualization (slower than the bare metal sandbox providers run on). Full tables and method: [/benchmarks](https://batonvm.vercel.app/benchmarks), raw data in [bench/results/](bench/results/).

| | p50 | p95 |
|---|---|---|
| New browser: request → Chrome answering CDP | **174 ms** | 197 ms |
| … when creates come back to back (100 in a row) | 231 ms | 267 ms |
| … when a warmed-up browser is waiting in the pool | ~4 ms | |
| Live migration: browser paused while it moves (20 moves) | **255 ms** | 279 ms |
| … a live CDP client's longest wait during the move | 609 ms | 705 ms |
| … disconnects | **0** | |
| Fork: parent paused | **209 ms** | 212 ms |
| Fork: 3 copies of a logged-in browser running | 2.6 s | 2.9 s |
| Pause a browser to S3 (request → stored and stopped) | 2.1 s | 2.3 s |
| Resume it, on any machine (request → Chrome answering) | **1.2 s** | 1.4 s |
| Start a session from a saved, logged-in profile | 0.8 s | 0.9 s |
| Memory a browser really costs (of 1 GiB): a restore / a fork copy / a warmed-up browser | 61 / ~110 / 230–280 MiB | |

The first row is 30 creates 2 s apart, measured separately: 16 restored a browser from the snapshot, and the other 14 were handed a warmed-up one. Back to back, the Firecracker processes each node starts ahead of time run out, and a restore starts its own. A warmed-up browser has already loaded a page, which is why it holds more of its own memory.

On [Browser Arena](https://www.browserarena.ai)'s open-source lifecycle benchmark (create, connect, load example.com, release; 100 sessions from us-east-1) a Baton session takes **470 ms**, from 1,198 ms at the start of one day of work on it (releasing in the background, a pool of warmed-up browsers, one round trip for the API). That is 6th of the 13 providers Browser Arena publishes raw results for (theirs, not re-run by us; the fastest is at 156 ms). What is left is mostly Chrome on nested virtualization; [/benchmarks](https://batonvm.vercel.app/benchmarks) says where the rest goes and what it would take.

On Steel's open-source [remote-browser benchmark](https://github.com/steel-dev/browserbench) (create, connect, load google.com, release; 300 runs from us-east-1) a Baton session takes **2,084 ms** at p50. Everything except the page load costs 735 ms, second-fastest of the six providers in [Steel's published table](https://steel.dev/blog/remote-browser-benchmark); the page load (1,358 ms) is where we lose, and [/benchmarks](https://batonvm.vercel.app/benchmarks) says why and what did not fix it. The other providers' numbers are Steel's, not re-run by us.

**The headline demos:**
- **Drain under load.** An agent clicks from article to article on Wikipedia while the machine its browser runs on is drained. Every step succeeds. One step is 1.6 s slower. Its tab's state survives. [`scripts/test/m5-headline.sh`](scripts/test/m5-headline.sh)
- **Fork a logged-in session.** One browser logs in, is forked into 6 across two machines, and six agents continue in parallel as the same user. [`scripts/test/m6-headline.sh`](scripts/test/m6-headline.sh)

## How it works

```
 agent (Playwright/Puppeteer) ──wss──▶ CloudFront ──▶ gateway ───── GRE tunnels ─────▶ nodes
 dashboard / REST API (Next.js on Vercel) ──────────▶   │ orchestrator (Go, SQLite)          │ agent (Go)
                                                         │ CDP proxy   (Go)                   │ jailed Firecracker VMs
                                                         │ NAT egress, routes per VM          │ (Chrome in each)
```

- **Fresh starts.** A browser is a restore of a base snapshot, not a boot. Its disk is a reflink clone, and its memory is the base's file mapped copy-on-write. The guest is told its own address and the time over vsock. Each node keeps a couple of jailed Firecracker processes started ahead of time.
- **Every VM has a stable address.** The gateway routes it to whichever node holds the VM, so a migration moves the route rather than the client's connection. The CDP proxy terminates the agent's WebSocket and never notices the move.
- **Post-copy live migration.** The pause sends only device state and a few KB of disk changes. The target resumes at once. Its memory is served by a `userfaultfd` handler that fetches 64 KiB blocks on fault and installs a background stream from the source's page server. Zero pages never travel.
- **Fork.** A running browser's snapshot becomes a base made on the fly. Copies map it copy-on-write, and spill to other nodes by pulling it once per node. Open tabs are reopened in fresh renderer processes, otherwise `Math.random` returns the same numbers in every copy.
- **The orchestrator** places VMs (spread or pack), owns the routes, runs two-phase migrations and drains, fences nodes that come back, and marks VMs lost when a node or a VM goes silent. Agents' clients are told at once instead of hanging.
- **Pause, resume and profiles.** A snapshot is stored in S3 as the VM's device state, its disk as the blocks that differ from the base every machine holds, and its memory as the non-zero pages in 32 MiB objects written and read in parallel. Resume rebuilds the disk, loads the memory into a sparse file, sets the guest clock and starts Chrome on whichever machine has room. A profile is the same snapshot taken from a running browser (it pauses ~205 ms); sessions from it are fork copies of an image each machine fetches once.
- **Autoscaling.** The orchestrator starts a stopped machine when the others are nearly full and, when load drops, drains one with live migration and stops it. Decisions are a pure function with unit tests. Machines configure their own network at boot, so nothing needs ssh.
- **Observability.** Every session has an event log (created, migrated with its pause, forked, paused, ended and why); Prometheus metrics on the orchestrator and every agent; a public status page.
- **Isolation:**
  - Every Firecracker runs under its **jailer**: chrooted to a jail holding only its own files, in its own PID namespace and cgroup v2 (memory capped, 1 CPU once running, pids capped), as an unprivileged user.
  - nftables keeps VMs off the cloud metadata service, the VPC, each other, mail ports and their hosts.
  - CDP access is by HMAC-signed, expiring session tokens.
  - Accounts have limits, checked under the same lock that places VMs.

## Problems worth reading about

- **Migration downtime from 2.9 s to about 250 ms**: post-copy over userfaultfd, then shaving the pause.
  - Preparing the target before pausing.
  - Sending disk changes ahead of time.
  - `fallocate` for the snapshot file.
  - Reading guest memory through `/proc/<pid>/mem` before the pause, so the snapshot doesn't fault it in.
- **The agent's stall is set by TCP, not by the pause**: retransmits back off 0.2/0.6/1.4 s. VM routes carry `rto_min 20ms`.
- **Connections broke on every second migration**: connection tracking doesn't migrate, so a returning VM's packets were INVALID.
- **`sudo` cost 300 ms inside the outage**: the orchestrator now holds `CAP_NET_ADMIN` instead.
- **Forked browsers were too identical**: the open tab's PRNG was copied, a reload didn't help, a new renderer did.
- **Serving a sparse image through `mmap` refilled its holes**; `pread` doesn't.
- **The jailer spent 34 ms waiting for RCU** while moving into a cgroup: `favordynmods`.
- **A CPU cap cost 75 ms per start**: VMs start uncapped and are capped once running.
- **Forked copies hung for 15 minutes**: a copy woke up with its parent's DevTools clients still connected, as far as Chrome knew, and Playwright's `waitForDebuggerOnStart` made every new tab wait for a client that would never answer. The guest agent now drops them when a copy wakes. Its first version found them by reading every process in `/proc`, which cost every new browser 130 ms (a fresh start went from 186 to 346 ms); now only restores look, and only in the bridge's cgroup.
- **A new base image filled the RAM disk**: snapshots, resumed sessions and profile images share a tmpfs with the bases, and the old base stayed loaded after the default moved on, so profile saves failed with "no space left on device". The agent now unloads bases no VM is made from.
- **systemd-logind deleted our snapshot memory** whenever an ssh session ended (`RemoveIPC`), and **an agent restart killed VMs mid-migration** (systemd's `KillMode`).

## Repository

| Path | What |
|---|---|
| [cmd/chromebox](cmd/chromebox) | one binary: node agent, orchestrator, CDP proxy, page-fault handler, CLI |
| [cmd/chromebox-guest](cmd/chromebox-guest) | the agent inside each VM: after a restore, it takes the VM's own address and the time, and drops DevTools clients left from before the snapshot |
| [internal/firecracker](internal/firecracker) | Firecracker API client (hand-written, no SDK) |
| [internal/vm](internal/vm), [internal/base](internal/base) | VM lifecycle, the jail, base snapshots, clones, fork images |
| [internal/uffd](internal/uffd), [internal/pageserver](internal/pageserver) | userfaultfd handler and the memory stream for post-copy |
| [internal/blockdiff](internal/blockdiff) | disk deltas from extent maps (FIEMAP) |
| [internal/orchestrator](internal/orchestrator), [internal/agent](internal/agent) | placement, migration, drain, fork, reconciliation, accounts |
| [internal/gateway](internal/gateway), [internal/token](internal/token) | CDP proxy, session options (browser overrides, egress through a customer's proxy), routes, session tokens |
| [web](web) | Next.js dashboard and public API (Vercel) |
| [sdk](sdk), [sdk-python](sdk-python) | `batonvm` for TypeScript (npm) and Python (PyPI) |
| [integrations](integrations) | the MCP server (`batonvm-mcp`), Stagehand and browser-use |
| [scripts](scripts) | AWS setup, host setup, guest images, tests and demos, benchmarks |

## Running it yourself

Everything runs on AWS EC2 (us-east-1): a t3.small gateway and m8i.large nodes with nested virtualization.
1. `scripts/aws/setup-account.sh`, then `scripts/aws/launch-node.sh` for each instance.
2. `scripts/host/setup.sh` on the nodes, `overlay.sh`, `firewall.sh` and `services.sh`.
3. Build the guest image (`scripts/guest/`) and a base (`chromebox base build`).
4. `scripts/deploy.sh <node>`.

The scripts are written for this one deployment rather than as a general installer.

## Known limits

- **One gateway** runs the orchestrator (SQLite), the CDP proxy and NAT, so it is a single point of failure. Sessions survive an orchestrator restart, not the loss of the gateway. A rebooted gateway restores its tunnels, NAT and firewall by itself.
- **Snapshot memory lives in RAM** (a 3.8 GiB tmpfs per node, shared by bases, resumed sessions, forks and profiles), so a burst of resumes and forks on one node can fill it.
- **Resume downloads all of a paused session's memory before it starts** (~1.0 s of its 1.2 s). Fetching pages from storage on demand, as post-copy migration does between machines, would cut that.
- **Nested virtualization** on EC2 rather than bare metal, and a 2-vCPU node fits about four browsers.
- **The page-fault handler** runs outside the jail.
- **A "view" token grants full CDP access.**

## License

[MIT](LICENSE)
