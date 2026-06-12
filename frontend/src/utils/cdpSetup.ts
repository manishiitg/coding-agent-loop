const defaultCdpPort = 9222

function safeCdpPort(port: number): number {
  return Number.isFinite(port) && port >= 1 && port <= 65535 ? Math.trunc(port) : defaultCdpPort
}

export function chromeCdpLaunchCommand(port: number, platform?: string): string {
  const resolvedPort = safeCdpPort(port)
  const userDataDir = '$HOME/.chrome-cdp-profile'
  const args = `--remote-debugging-port=${resolvedPort} --user-data-dir="${userDataDir}" --no-first-run --no-default-browser-check`

  if (platform?.includes('Mac')) {
    return `/Applications/Google\\ Chrome.app/Contents/MacOS/Google\\ Chrome ${args}`
  }

  return `google-chrome ${args}`
}

export function chromeCdpVerifyCommand(port: number): string {
  return `curl http://127.0.0.1:${safeCdpPort(port)}/json/version`
}
