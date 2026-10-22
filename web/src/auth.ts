import NextAuth, { CredentialsSignin } from 'next-auth';
import Credentials from 'next-auth/providers/credentials';
import { orch } from '@/lib/orch';

declare module 'next-auth' {
  interface Session { uid?: string; login?: string }
}

// The reason a device-flow check did not sign the user in yet. "pending" and
// "slow_down" mean: ask again later; the rest end the attempt.
class DeviceFlow extends CredentialsSignin {
  constructor(code: string) { super(); this.code = code; }
}

// GitHub login through the device flow (the one GitHub designed for devices
// without a browser). The sign-in page shows a short code; the user enters it
// at github.com/login/device; this server then exchanges the device code for a
// GitHub token using only the app's public client ID. No client secret exists
// anywhere. The token is used once, to read the user's GitHub id and login,
// and is not kept. On first sign-in the GitHub account becomes a Baton user.
export const { handlers, auth, signIn, signOut } = NextAuth({
  providers: [
    Credentials({
      id: 'github-device',
      credentials: { deviceCode: {} },
      async authorize(c) {
        const r = await fetch('https://github.com/login/oauth/access_token', {
          method: 'POST',
          headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
          body: JSON.stringify({
            client_id: process.env.AUTH_GITHUB_ID,
            device_code: c.deviceCode,
            grant_type: 'urn:ietf:params:oauth:grant-type:device_code',
          }),
        });
        const t = await r.json();
        if (!t.access_token) throw new DeviceFlow(t.error === 'authorization_pending' ? 'pending' : (t.error ?? 'failed'));
        const gh = await fetch('https://api.github.com/user', {
          headers: { Authorization: `Bearer ${t.access_token}`, 'User-Agent': 'baton' },
        }).then((x) => x.json());
        if (!gh.id) throw new DeviceFlow('failed');
        const u = await orch<{ id: string }>('POST', '/v1/users', { github_id: gh.id, login: gh.login });
        if (!u.ok) throw new DeviceFlow('failed');
        return { id: u.data.id, name: gh.login };
      },
    }),
  ],
  pages: { signIn: '/' },
  callbacks: {
    async jwt({ token, user }) {
      if (user) { token.uid = user.id; token.login = user.name ?? undefined; }
      return token;
    },
    async session({ session, token }) {
      session.uid = token.uid as string | undefined;
      session.login = token.login as string | undefined;
      return session;
    },
  },
});
