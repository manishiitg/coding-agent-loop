import { defineConfig } from '@playwright/test'
export default defineConfig({
 testDir: '.', testMatch: 'live.spec.ts', workers: 1, timeout: 30000,
 outputDir: process.env.AGENTWORKS_TEST_OUTPUT || '../test-results',
 reporter: [['list']], use: { viewport: { width: 640, height: 480 }, headless: true },
})
