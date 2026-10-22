// POST /api/auth/device starts a GitHub device-flow sign-in: GitHub returns a
// short code for the user to enter at github.com/login/device, and a device
// code that this browser then presents to sign in once the user has approved.
export async function POST() {
  const r = await fetch('https://github.com/login/device/code', {
    method: 'POST',
    headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    body: JSON.stringify({ client_id: process.env.AUTH_GITHUB_ID }),
  });
  const d = await r.json();
  if (!d.device_code) return Response.json({ error: d.error_description ?? 'GitHub refused the sign-in' }, { status: 502 });
  return Response.json({
    deviceCode: d.device_code,
    userCode: d.user_code,
    verificationUri: d.verification_uri,
    interval: d.interval,
    expiresIn: d.expires_in,
  });
}
