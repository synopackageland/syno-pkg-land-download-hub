import { appOpenSmoke } from '@synopackageland/cli/e2e';

// A (administrator) and N (a user Application Privileges admit) reach the
// downloads page; X (signed out) stays at DSM's gate.
appOpenSmoke({ ready: '[data-downloads]' });
