import NextAuth from 'next-auth';
import GitHub from 'next-auth/providers/github';
import { orch } from '@/lib/orch';

declare module 'next-auth' {
  interface Session { uid?: string; login?: string }
}

// GitHub login. On first sign-in the GitHub account becomes a Baton user (in the
// orchestrator's database); the user id rides in the signed session cookie.
export const { handlers, auth, signIn, signOut } = NextAuth({
  providers: [GitHub],
  callbacks: {
    async jwt({ token, profile }) {
      if (profile) {
        const r = await orch<{ id: string }>('POST', '/v1/users', { github_id: Number(profile.id), login: profile.login });
        if (r.ok) { token.uid = r.data.id; token.login = profile.login as string; }
      }
      return token;
    },
    async session({ session, token }) {
      session.uid = token.uid as string | undefined;
      session.login = token.login as string | undefined;
      return session;
    },
  },
});
