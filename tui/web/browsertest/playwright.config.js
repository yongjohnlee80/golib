// Playwright configuration for the ADR-0009 §2.9 browser matrix.
//
// All three engines are REQUIRED projects. A run that silently skips one is the
// failure mode the matrix exists to prevent, so nothing here is conditional on a
// browser being installed — an absent engine fails loudly.
import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  // No retries: a flaky text-machine assertion is a real finding about an
  // engine, and retrying would average it away.
  retries: 0,
  fullyParallel: false,
  workers: 1,
  reporter: [['list'], ['json', { outputFile: 'results.json' }]],
  timeout: 30_000,
  expect: { timeout: 5_000 },
  use: {
    // The fixture server binds loopback and prints its URL; each test navigates
    // with a fresh single-use ticket.
    baseURL: process.env.WEBTUI_URL || 'http://127.0.0.1:8081',
    trace: 'retain-on-failure',
  },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
    { name: 'firefox', use: { ...devices['Desktop Firefox'] } },
    { name: 'webkit', use: { ...devices['Desktop Safari'] } },
  ],
  webServer: {
    // Built and run by Go, so the harness exercises the real server rather than
    // a mock of it.
    command: 'go run ./fixture',
    url: process.env.WEBTUI_URL || 'http://127.0.0.1:8081/healthz',
    reuseExistingServer: false,
    timeout: 60_000,
    cwd: '.',
  },
});
