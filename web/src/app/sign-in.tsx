'use client';
import { useRef, useState } from 'react';
import { signIn } from 'next-auth/react';

type Start = { deviceCode: string; userCode: string; verificationUri: string; interval: number; expiresIn: number };

// Sign in with GitHub's device flow: show the code, send the user to GitHub to
// enter it, and check back every few seconds until GitHub says yes or no.
export function useDeviceSignIn() {
  const [start, setStart] = useState<Start | null>(null);
  const [error, setError] = useState('');
  const [copied, setCopied] = useState(false);
  const attempt = useRef(0); // a cancel or a new start ends the previous attempt's polling

  const begin = async () => {
    const mine = ++attempt.current;
    setError('');
    const r = await fetch('/api/auth/device', { method: 'POST' });
    const s = await r.json();
    if (!r.ok) { setError(s.error); return; }
    setStart(s);
    let wait = s.interval;
    const deadline = Date.now() + s.expiresIn * 1000;
    while (Date.now() < deadline) {
      await new Promise((ok) => setTimeout(ok, wait * 1000));
      if (attempt.current !== mine) return;
      const res = await signIn('github-device', { deviceCode: s.deviceCode, redirect: false });
      if (res?.ok && !res.error) { location.href = '/dashboard'; return; }
      if (res?.code === 'pending') continue;
      if (res?.code === 'slow_down') { wait += 5; continue; }
      setError(res?.code === 'access_denied' ? 'Sign-in was cancelled on GitHub.' : 'Sign-in failed. Try again.');
      setStart(null);
      return;
    }
    if (attempt.current !== mine) return;
    setError('The code expired. Try again.');
    setStart(null);
  };
  const copy = () => start && navigator.clipboard.writeText(start.userCode).then(() => setCopied(true));
  const cancel = () => { attempt.current++; setStart(null); };
  return { start, error, copied, begin, copy, cancel };
}
