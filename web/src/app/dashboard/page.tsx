import { auth, signOut } from '@/auth';
import { redirect } from 'next/navigation';
import Dashboard from './ui';

export default async function Page() {
  const session = await auth();
  if (!session?.uid) redirect('/');
  return (
    <main className="mx-auto max-w-4xl px-4 py-10">
      <header className="flex items-center justify-between gap-4">
        <h1 className="text-xl font-semibold">Baton <span className="font-normal text-zinc-500">· {session.login}</span></h1>
        <form action={async () => { 'use server'; await signOut({ redirectTo: '/' }); }}>
          <button className="text-sm text-zinc-500 hover:underline">Sign out</button>
        </form>
      </header>
      <Dashboard />
    </main>
  );
}

// Reads the session cookie, so it is rendered per request.
export const instant = false;
