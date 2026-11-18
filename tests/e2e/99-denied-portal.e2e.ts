// The Application Portal for normal1 without Application Privileges. DSM signs
// the session out when it refuses the portal, so this file runs last and then
// drops normal1's saved profile: the next run signs normal1 in again.
import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { rm } from 'node:fs/promises';
import { handleDialogs, openDesktop, openPortal, profilePath } from '@synopackageland/cli/e2e';
import { call, temporary } from './support/download-hub.ts';

// DSM's launcher shows this alert before it loads any App JS, so the
// App cannot explain the refusal itself: the alert is known DSM behaviour.
const refused = 'You are not authorized to use this service.';

test('normal1 without Application Privileges gets DSM\'s refusal in the portal and polls nothing', { session: 'normal1', timeout: 120_000 }, async ({ app, browser }) => {
  const page = await openDesktop(app);
  test.skip((await call(page, 'gopeed.session')).success, 'normal1 has Application Privileges now');
  const alerts = await handleDialogs(browser);
  let polls = 0;
  page.on('request', request => { if (request.url().includes('/SynoPkgGopeed/cgi/api.cgi')) polls++; });
  try {
    await openPortal(app, '/download-hub/', browser);
    await expect.poll(() => alerts.length, { timeout: 15_000 }).toBeGreaterThan(0);
    expect(alerts).toEqual([refused]);
    // DSM may already have left the portal; whatever page remains says nothing temporary and polls nothing.
    expect(await page.getByText(temporary).count()).toBe(0);
    const before = polls;
    await page.waitForTimeout(5_000);
    expect(polls).toBe(before);
  } finally {
    await page.context().clearCookies();
    await rm(profilePath(process.env.DSM_E2E_TARGET || 'test-nas', 'normal1'), { force: true });
  }
});
