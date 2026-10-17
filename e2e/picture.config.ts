import base from './playwright.config';

// A Go composition test starts and stops this fixture; the normal journey
// configuration excludes this fixture directory.
export default {
  ...base,
  testDir: './picture',
  testIgnore: [],
  testMatch: 'composed-picture.spec.ts',
};
