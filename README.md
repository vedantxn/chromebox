# Baton

Cloud browsers for AI agents. Every session is a real Chrome in its own Firecracker microVM, restored from a snapshot in about 190 ms. A running browser can **move to another machine mid-session** without the agent's connection dropping, be **forked** into copies that start from the same logged-in state, **paused** to storage (and cost nothing) and resumed on any machine, and **saved as a profile** so later sessions start already logged in. Machines are **started and stopped automatically** by load.

- Live: **https://batonvm.vercel.app** (sign in with GitHub, get an API key)
- SDKs: `npm install batonvm` ([sdk/](sdk/)) · `pip install batonvm` ([sdk-python/](sdk-python/), sync and async)
- Numbers, with raw samples: **https://batonvm.vercel.app/benchmarks** · live status: **https://batonvm.vercel.app/status**

```js
import { Baton } from 'batonvm';
import { chromium } from 'playwright-core';

const baton = new Baton(); // BATON_API_KEY
const session = await baton.sessions.create();
const browser = await chromium.connectOverCDP(session.connectUrl);
const page = browser.contexts()[0].pages()[0] ?? await browser.contexts()[0].newPage();
await page.goto('https://example.com');

await session.migrate();             // the browser changes machines; `page` keeps working
const { sessions } = await session.fork(3); // 3 copies: same pages, cookies, storage

await session.pause();                      // stored in S3; no machine holds it
await session.resume();                     // back on any machine, exactly as it was
await session.saveProfile('shop');          // later: baton.sessions.create({ profile: 'shop' })
```

## What it does, measured

From a client in us-east-1, on 2 × m8i.large with nested virtualization (slower than the bare metal sandbox providers run on). Full tables and method: [/benchmarks](https://batonvm.vercel.app/benchmarks), raw data in [bench/results/](bench/results/).

| | p50 | p95 |
|---|---|---|
| New browser: request → Chrome answering CDP (100 creates) | **186 ms** | 209 ms |
| Live migration: browser paused while it moves (20 moves) | **212 ms** | 241 ms |
| … a live CDP client's longest wait during the move | 447 ms | 877 ms |
| … disconnects | **0** | |
| Fork: parent paused | **210 ms** | 215 ms |
| Fork: 3 copies of a logged-in browser running | 2.6 s | 2.7 s |
| Pause a browser to S3 (request → stored and stopped) | 1.9 s | 2.1 s |
| Resume it, on any machine (request → Chrome answering) | **1.0 s** | 1.1 s |
| Start a session from a saved, logged-in profile | 0.8 s | 0.9 s |
| Memory a browser really costs (of 1 GiB): fresh / fork copy | 55 / 84 MiB | |

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

- **Migration downtime from 2.9 s to ~210 ms**: post-copy over userfaultfd, then shaving the pause.
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
- **systemd-logind deleted our snapshot memory** whenever an ssh session ended (`RemoveIPC`), and **an agent restart killed VMs mid-migration** (systemd's `KillMode`).

## Repository

| Path | What |
|---|---|
| [cmd/chromebox](cmd/chromebox) | one binary: node agent, orchestrator, CDP proxy, page-fault handler, CLI |
| [internal/firecracker](internal/firecracker) | Firecracker API client (hand-written, no SDK) |
| [internal/vm](internal/vm), [internal/base](internal/base) | VM lifecycle, the jail, base snapshots, clones, fork images |
| [internal/uffd](internal/uffd), [internal/pageserver](internal/pageserver) | userfaultfd handler and the memory stream for post-copy |
| [internal/blockdiff](internal/blockdiff) | disk deltas from extent maps (FIEMAP) |
| [internal/orchestrator](internal/orchestrator), [internal/agent](internal/agent) | placement, migration, drain, fork, reconciliation, accounts |
| [internal/gateway](internal/gateway), [internal/token](internal/token) | CDP proxy, routes, session tokens |
| [web](web) | Next.js dashboard and public API (Vercel) |
| [sdk](sdk) | `batonvm`, the TypeScript SDK |
| [scripts](scripts) | AWS setup, host setup, guest images, tests and demos, benchmarks |

## Running it yourself

Everything runs on AWS EC2 (us-east-1): a t3.small gateway and m8i.large nodes with nested virtualization.
1. `scripts/aws/setup-account.sh`, then `scripts/aws/launch-node.sh` for each instance.
2. `scripts/host/setup.sh` on the nodes, `overlay.sh`, `firewall.sh` and `services.sh`.
3. Build the guest image (`scripts/guest/`) and a base (`chromebox base build`).
4. `scripts/deploy.sh <node>`.

The scripts are written for this one deployment rather than as a general installer.

## Known limits

- **One gateway** runs the orchestrator (SQLite), the CDP proxy and NAT, so it is a single point of failure. Sessions survive an orchestrator restart, not a gateway loss. Its own network setup (tunnels, NAT, rules) is not yet restored at boot the way the nodes' is.
- **Resume downloads all of a paused session's memory before it starts** (~0.8 s of its 1.0 s). Fetching pages from storage on demand, as post-copy migration does between machines, would cut that.
- **Nested virtualization** on EC2 rather than bare metal, and a 2-vCPU node fits about four browsers.
- **The page-fault handler** runs outside the jail.
- **A "view" token grants full CDP access.**

## License

[MIT](LICENSE)
