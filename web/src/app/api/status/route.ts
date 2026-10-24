import { getStats } from '@/lib/stats';

// GET /api/status: the same numbers as the status page, as JSON. Public; no
// user, session or address appears in it.
export async function GET() {
  const s = await getStats();
  return Response.json(s ?? { error: 'control plane not answering' }, {
    status: s ? 200 : 503,
    headers: { 'Cache-Control': 'public, s-maxage=10, stale-while-revalidate=30' },
  });
}
