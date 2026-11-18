// One DSM session per account for the whole run, restored from the shared
// profile while DSM still accepts it. A profile DSM has signed out makes the
// desktop reload as it loads, which fails the restore (ERR_ABORTED), so it is
// checked first and dropped. A profile can also reach the desktop without a
// CSRF token (SYNO.SDS.Session.SynoToken), for example one another tool saved
// from an API sign-in; the App then refuses every call with
// session_required, so such a profile is replaced by a fresh sign-in.
import { test } from '@e2e-dev/web';
import { rm } from 'node:fs/promises';
import { dsmPage, dsmUrl, loadProfile, profilePath, signIn } from '@synopackageland/cli/e2e';

const hasSynoToken = () => dsmPage().evaluate(() => !!(window as any).SYNO?.SDS?.Session?.SynoToken);

/** Whether DSM still has the session of a saved profile. */
async function stillSignedIn(file: string): Promise<boolean> {
  const profile: any = loadProfile(file, dsmUrl());
  if (!profile) return false;
  const cookie = (profile.cookies ?? []).map((c: { name: string; value: string }) => `${c.name}=${c.value}`).join('; ');
  const reply = await fetch(new URL('/webman/login.cgi', dsmUrl()), { headers: { Cookie: cookie } }).then(r => r.json(), () => ({}));
  return reply.success === true;
}

for (const user of ['admin', 'normal1']) {
  test.setup(`DSM sign-in as ${user}`, { sessions: [user], timeout: 180_000 }, async ({ app, session }) => {
    const file = profilePath(process.env.DSM_E2E_TARGET || 'test-nas', user);
    if (!await stillSignedIn(file)) await rm(file, { force: true });
    if (await signIn(app, user) === 'restored' && !await hasSynoToken()) {
      await dsmPage().context().clearCookies();
      await rm(file, { force: true });
      await signIn(app, user);
    }
    await session.save(user);
  });
}
