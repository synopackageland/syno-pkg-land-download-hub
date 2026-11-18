import { test } from '@e2e-dev/web';
import { signIn } from '@synopackageland/cli/e2e';

test.setup('Coverage DSM administrator sign-in', { sessions: ['admin'], timeout: 180_000 }, async ({ app, session }) => {
  await signIn(app, 'admin');
  await session.save('admin');
});
