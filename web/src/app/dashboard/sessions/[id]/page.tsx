import { auth } from '@/auth';
import { redirect } from 'next/navigation';
import Link from 'next/link';
import Live from './live';

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const session = await auth();
  if (!session?.uid) redirect('/');
  const { id } = await params;
  return (
    <main className="mx-auto max-w-5xl px-4 py-8">
      <Link href="/dashboard" className="text-sm text-zinc-500 hover:underline">← Sessions</Link>
      <h1 className="mt-2 font-mono text-lg">{id}</h1>
      <Live id={id} />
    </main>
  );
}

export const instant = false;
