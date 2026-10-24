import { Schibsted_Grotesk } from 'next/font/google';
import Link from 'next/link';
import { connection } from 'next/server';
import { auth } from '@/auth';
import Handoff from './landing/Handoff';
import Abilities from './landing/Abilities';
import Motion from './landing/Motion';
import { StartBand, StartButton } from './landing/Start';
import { arena } from './landing/data';
import './landing/landing.css';

const grotesk = Schibsted_Grotesk({ subsets: ['latin'], weight: ['400', '600', '800'], variable: '--font-lp' });

const repo = 'https://github.com/vedantxn/chromebox';

const code = `import { Baton } from 'batonvm';
import { chromium } from 'playwright-core';

const baton = new Baton(); // BATON_API_KEY
const session = await baton.sessions.create();
const browser = await chromium.connectOverCDP(session.connectUrl);
const page = browser.contexts()[0].pages()[0];
await page.goto('https://example.com');

await session.migrate();          // another machine; page keeps working
await session.fork(3);            // three copies, same cookies
await session.pause();            // stored; no machine holds it
await session.resume();           // back, on any machine`;

const boards = [...arena.published.map((p) => ({ ...p, ours: false })), { name: 'Baton', total: arena.baton.total, date: arena.baton.date, ours: true }]
  .sort((a, b) => a.total - b.total);

export default async function Home() {
  await connection(); // per request: it reads the session cookie
  const session = await auth();
  const signedIn = Boolean(session?.uid);
  return (
    <div className={`lp ${grotesk.variable}`}>
      <Motion />
      <header className="nav">
        <Link href="/" className="nav__mark">Baton</Link>
        <nav aria-label="Main">
          <Link href="/docs">Docs</Link>
          <Link href="/benchmarks">Benchmarks</Link>
          <Link href="/status">Status</Link>
          <a href={repo}>GitHub</a>
        </nav>
      </header>

      <main>
        <section className="hero">
          <div className="hero__text">
            <h1 className="lines">
              <span className="line"><span data-line style={{ '--i': 0 } as React.CSSProperties}>Cloud browsers</span></span>
              <span className="line"><span data-line style={{ '--i': 1 } as React.CSSProperties}>that change hands</span></span>
            </h1>
            <p className="hero__lead" data-load>
              Each browser runs in its own Firecracker microVM. Drain the machine under it and it moves to another
              mid-session, with your agent still connected.
            </p>
            <div data-load className="hero__action"><StartButton signedIn={signedIn} /></div>
          </div>
          <div className="hero__stage">
            <Handoff />
          </div>
        </section>

        <section className="showcase" aria-label="What a session can do">
          <Abilities />
        </section>

        <section className="features" aria-labelledby="features-title">
          <h2 id="features-title" className="sr-only">Built to be moved</h2>
          <p className="statement" data-reveal>
            Drain a machine{' '}
            <span className="slot"><img src="/landing/statelibqld-1-105872-runners-in-a-relay.jpg" alt="Two runners passing a baton, Brisbane, 1939" width={1600} height={1248} loading="lazy" /></span>{' '}
            and its browsers carry on while agents work. Machines with nothing to do{' '}
            <span className="slot slot--ui" data-ui><span className="chip">node-b · stopped</span></span>{' '}
            stop by themselves. Through all of it a session{' '}
            <span className="slot"><img src="/landing/2024-defender-cup-competes-at-vandenberg-8275168.jpg" alt="A baton handed over mid-stride, Vandenberg, 2024" width={1600} height={1067} loading="lazy" style={{ objectPosition: '50% 32%' }} /></span>{' '}
            keeps its pages, cookies and address.
          </p>
          <div className="features__below" data-reveal>
            <figure className="capsule-photo">
              <span className="capsule-photo__frame"><img className="drift" src="/landing/1912-athletics-men-s-4x100-metre-final2.jpg" alt="The finish of the 4 × 100 metres relay final, Stockholm Olympics, 1912" width={1600} height={1122} loading="lazy" /></span>
              <figcaption>The 4 × 100 m relay final, Stockholm, 1912.</figcaption>
            </figure>
            <div className="features__note">
              <p>
                Scaling down is a live migration too. In our autoscaling test a client kept sending requests while the
                autoscaler drained its machine: 664 requests, none failed. The empty machine then switched itself off.
              </p>
              <a className="link" href={`${repo}/tree/main/docs/learn`}>Read the engineering notes</a>
            </div>
          </div>
        </section>

        <div className="band" data-reveal>
          <video className="band__film" autoPlay muted loop playsInline preload="metadata" poster="/landing/relay-1939-poster.jpg" width={960} height={720} aria-hidden="true">
            <source src="/landing/relay-1939.mp4" type="video/mp4" />
          </video>
          <div className="band__text">
            <p>Every figure on this page was measured from us-east-1, and the raw samples are public.</p>
            <Link className="band__link" href="/benchmarks">See the benchmarks</Link>
          </div>
        </div>

        <section className="proof" aria-labelledby="proof-title">
          <ul className="tiles" data-reveal>
            <li className="tile tile--head">
              <h2 id="proof-title">Measured on Browser Arena</h2>
              <p>
                Create, connect, load example.com, release: the sum of the medians over 100 sessions in a row.
                Ours went from {arena.baton.before.toLocaleString('en-US')} to {arena.baton.total} ms in one day.
              </p>
            </li>
            {boards.map((b) => (
              <li key={b.name} className={`tile${b.ours ? ' tile--ours' : ''}`}>
                <span className="tile__name">{b.name}</span>
                <span className="tile__figure">{b.total.toLocaleString('en-US')} ms</span>
                <span className="tile__note">{b.ours ? `our run, ${arena.baton.ok} of ${arena.baton.runs} ok` : `their run, ${b.date.slice(5).replace('-', '/')}`}</span>
              </li>
            ))}
          </ul>
          <p className="proof__note">
            We ran Baton through Browser Arena&apos;s own harness on {arena.baton.date} from us-east-1: create {arena.baton.create} ms,
            connect {arena.baton.connect} ms, load {arena.baton.load} ms, release {arena.baton.release} ms, p95 {arena.baton.p95} ms.
            The other figures are Browser Arena&apos;s own latest runs, from the raw results it publishes; we did not re-run them.
          </p>
        </section>

        <section className="code" aria-labelledby="code-title">
          <div className="code__text" data-reveal>
            <h2 id="code-title">Plain Playwright</h2>
            <p>
              Install the SDK, create a session and connect with the browser library you already use. The same session
              object moves, forks, sleeps and wakes.
            </p>
            <p className="code__install">npm install batonvm playwright-core</p>
            <Link className="link" href="/docs">The quickstart and the REST API</Link>
            <figure className="code__photo">
              <span className="capsule-photo__frame"><img src="/landing/the-medlay-relay-baton-handover.jpg" alt="A baton handed over on a red track during a medley relay" width={1600} height={1066} loading="lazy" /></span>
            </figure>
          </div>
          <pre className="code__block" data-reveal><code>{code}</code></pre>
        </section>

        <section className="life" aria-labelledby="life-title">
          <div className="field field--1">
            <h2 id="life-title">A session&apos;s life</h2>
            <dl className="card" data-ui>
              <dt>vm-7f3a21</dt><dd>warm → running</dd>
              <dt>node</dt><dd>node-a</dd>
              <dt>handed out</dt><dd>{arena.baton.create} ms</dd>
            </dl>
            <p className="field__clause">First it starts warm, already running.</p>
          </div>
          <div className="field field--2">
            <dl className="card" data-ui>
              <dt>vm-7f3a21</dt><dd>running</dd>
              <dt>node</dt><dd>node-a → node-b</dd>
              <dt>paused</dt><dd>212 ms</dd>
            </dl>
            <p className="field__clause">Then it changes machines.</p>
          </div>
          <div className="field field--3">
            <dl className="card" data-ui>
              <dt>vm-7f3a21</dt><dd>forked</dd>
              <dt>copies</dt><dd>3 · node-a, node-b</dd>
              <dt>all running</dt><dd>2.6 s</dd>
            </dl>
            <p className="field__clause">Then it splits in three.</p>
          </div>
          <div className="field field--4">
            <dl className="card" data-ui>
              <dt>vm-7f3a21</dt><dd>paused</dd>
              <dt>stored</dt><dd>602 MiB in S3</dd>
              <dt>resumes in</dt><dd>1.0 s</dd>
            </dl>
            <p className="field__clause">Then it sleeps in storage.</p>
          </div>
        </section>

        <StartBand signedIn={signedIn} />
      </main>

      <footer className="foot">
        <div className="foot__mark" aria-label="Baton">
          {['B', 'A', 'T', 'O', 'N'].map((l, k) => (
            <span key={k} aria-hidden="true" style={{ height: `${[100, 72, 88, 64, 94][k]}%` }}>{l}</span>
          ))}
        </div>
        <div className="foot__links">
          <ul><li><Link href="/docs">Docs</Link></li><li><Link href="/benchmarks">Benchmarks</Link></li><li><Link href="/status">Status</Link></li></ul>
          <ul><li><a href={repo}>Source on GitHub</a></li><li><a href="https://www.npmjs.com/package/batonvm">batonvm on npm</a></li><li><a href={`${repo}/tree/main/docs/learn`}>Engineering notes</a></li></ul>
          <ul><li><a href="/benchmarks.json">Raw benchmark data</a></li><li><a href="/browserbench-baton.jsonl">Browserbench runs</a></li></ul>
          <p className="foot__contact">
            Built by Vedant Nayak on AWS us-east-1.
            <br />
            <a href="https://github.com/vedantxn">github.com/vedantxn</a>
          </p>
        </div>
        <p className="foot__credits">
          Photographs: runners in a relay race, Brisbane, 1939, by the Sunday Truth (State Library of Queensland, public
          domain); the 4 × 100 m relay final, Stockholm, 1912 (public domain); the 2024 Defender Cup at Vandenberg, by Senior
          Airman Joshua LeRoi, U.S. Space Force (public domain), all via Wikimedia Commons; “The Medlay Relay Baton
          Handover” by Óglaigh na hÉireann (Flickr, CC BY 2.0). Film: “Estafetteloop voor militairen”, Polygoon Hollands
          Nieuws, 1939 (Netherlands Institute for Sound and Vision, public domain), cut to twelve seconds. All shown in
          black and white.
        </p>
        <div className="foot__last">
          <span>© 2026 Baton</span>
          <Link className="button button--small" href="/docs">Read the docs</Link>
        </div>
      </footer>
    </div>
  );
}

// Reads the session cookie, so it is rendered per request.
export const instant = false;
