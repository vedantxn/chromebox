'use client';
import { useDeviceSignIn } from '../sign-in';

// The page's one action: sign in with GitHub (device flow), shown either as the
// hero's button or as the closing band. Signed-in visitors go to the dashboard.

function CodePanel({ s }: { s: ReturnType<typeof useDeviceSignIn> }) {
  if (!s.start) return null;
  return (
    <div className="signin-panel" role="dialog" aria-label="Sign in with GitHub">
      <p>Enter this code on GitHub:</p>
      <button type="button" className="signin-panel__code" onClick={s.copy}>{s.start.userCode}</button>
      <p className="signin-panel__hint">{s.copied ? 'Copied.' : 'Select it to copy.'}</p>
      <p><a className="link" href={s.start.verificationUri} target="_blank" rel="noreferrer">Open github.com/login/device</a></p>
      <p className="signin-panel__hint">This page continues by itself once GitHub says yes.</p>
      <button type="button" className="link signin-panel__cancel" onClick={s.cancel}>Cancel</button>
    </div>
  );
}

export function StartButton({ signedIn }: { signedIn: boolean }) {
  const s = useDeviceSignIn();
  if (signedIn) return <a className="button" href="/dashboard">Open the dashboard</a>;
  return (
    <div className="start">
      <button type="button" className="button" onClick={s.begin}>Get an API key</button>
      {s.error && <p className="start__error">{s.error}</p>}
      <CodePanel s={s} />
    </div>
  );
}

const Arrow = () => (
  <svg className="cta__arrow" viewBox="0 0 48 48" aria-hidden="true"><path d="M12 36 36 12M16 12h20v20" fill="none" stroke="currentColor" strokeWidth="3" /></svg>
);

export function StartBand({ signedIn }: { signedIn: boolean }) {
  const s = useDeviceSignIn();
  const inner = (
    <>
      <span className="cta__line">{signedIn ? 'Open your dashboard: keys, sessions and their events.' : 'Sign in with GitHub and get an API key. Free: two browsers at a time, fifteen minutes each.'}</span>
      <Arrow />
      <span className="cta__word">{signedIn ? 'Open' : 'Sign in'}</span>
    </>
  );
  return (
    <section className="cta" aria-labelledby="cta-title">
      <h2 id="cta-title" className="sr-only">Get started</h2>
      {signedIn
        ? <a className="cta__band" href="/dashboard">{inner}</a>
        : <button type="button" className="cta__band" onClick={s.begin}>{inner}</button>}
      {s.error && <p className="start__error">{s.error}</p>}
      <CodePanel s={s} />
    </section>
  );
}
