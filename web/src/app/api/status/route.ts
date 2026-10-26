import { connection } from 'next/server';
import { getStats } from '@/lib/stats';

// GET /api/status: the same numbers as the status page, as JSON. Public; no
// user, session or address appears in it.
export async function GET() {
  // Per request: without it the handler reads nothing from the request, so it
  // is rendered once at build and the numbers never change. getStats still
  // caches for 30 s across visitors.
  await connection();
  const s = await getStats();
  return Response.json(s ?? { error: 'control plane not answering' }, {
    status: s ? 200 : 503,
    headers: { 'Cache-Control': 'public, s-maxage=10, stale-while-revalidate=30' },
  });
}
