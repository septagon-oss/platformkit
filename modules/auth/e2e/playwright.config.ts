import { tmpdir } from 'node:os';
import { join } from 'node:path';

export default {
  testDir: '.', timeout: 20_000, workers: 1, retries: 0,
  reporter: 'list', use: { headless: true },
  outputDir: join(tmpdir(), 'auth-mail-feedback-results'),
};
