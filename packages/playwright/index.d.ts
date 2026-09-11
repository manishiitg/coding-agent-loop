import type { BrowserContext } from '@playwright/test'
export { expect } from '@playwright/test'
export declare const test: typeof import('@playwright/test').test
export interface LiveBrowserOptions {
  apiURL?: string
  token?: string
  sessionID?: string
  label?: string
}
export declare function attachLiveBrowser(context: BrowserContext, options?: LiveBrowserOptions): Promise<{
  sessionID: string
  stop(): Promise<void>
  readonly warning: string
}>
