'use client';
import { useState } from 'react';
import { signIn } from 'next-auth/react';

type Start = { deviceCode: string; userCode: string; verificationUri: string; interval: number; expiresIn: number };

// Sign in with GitHub's device flow: show the code, send the user to GitHub to
// enter it, and check back every few seconds until GitHub says yes or no.
export function useDeviceSignIn() {
  const [start, setStart] = useState<Start | null>(null);
  const [error, setError] = useState('');
  const [copied, setCopied] = useState(false);

  const begin = async () => {
    setError('');
    const r = await fetch('/api/auth/device', { method: 'POST' });
    const s = await r.json();
    if (!r.ok) { setError(s.error); return; }
    setStart(s);
    let wait = s.interval;
    const deadline = Date.now() + s.expiresIn * 1000;
    while (Date.now() < deadline) {
      await new Promise((ok) => setTimeout(ok, wait * 1000));
      const res = await signIn('github-device', { deviceCode: s.deviceCode, redirect: false });
      if (res?.ok && !res.error) { location.href = '/dashboard'; return; }
      if (res?.code === 'pending') continue;
      if (res?.code === 'slow_down') { wait += 5; continue; }
      setError(res?.code === 'access_denied' ? 'Sign-in was cancelled on GitHub.' : 'Sign-in failed. Try again.');
      setStart(null);
      return;
    }
    setError('The code expired. Try again.');
    setStart(null);
  };
  const copy = () => start && navigator.clipboard.writeText(start.userCode).then(() => setCopied(true));
  const cancel = () => setStart(null);
  return { start, error, copied, begin, copy, cancel };
}

export default function SignIn() {
  const { start, error, copied, begin, copy } = useDeviceSignIn();
  if (!start) {
    return (
      <div>
        <button onClick={begin} className="rounded-md bg-zinc-900 px-4 py-2 text-sm font-medium text-white dark:bg-white dark:text-zinc-900">
          Sign in with GitHub
        </button>
        {error && <p className="mt-2 text-sm text-red-600">{error}</p>}
      </div>
    );
  }
  return (
    <div className="rounded-lg border border-zinc-200 p-4 text-sm dark:border-zinc-800">
      <p>Enter this code on GitHub:</p>
      <button onClick={copy} className="mt-2 font-mono text-2xl font-semibold tracking-widest" title="Copy">
        {start.userCode}
      </button>
      <span className="ml-2 text-xs text-zinc-500">{copied ? 'copied' : 'click to copy'}</span>
      <p className="mt-3">
        <a href={start.verificationUri} target="_blank" rel="noreferrer" className="font-medium underline">
          Open github.com/login/device
        </a>
      </p>
      <p className="mt-3 text-xs text-zinc-500">Waiting for GitHub… this page continues by itself.</p>
    </div>
  );
}
