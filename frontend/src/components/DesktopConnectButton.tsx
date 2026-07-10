import { useState } from 'react'
import { Bell, Copy, Download, ExternalLink, Loader2, MonitorDown, X } from 'lucide-react'
import { Button } from './ui/Button'
import { Tooltip, TooltipContent, TooltipTrigger } from './ui/tooltip'
import { authApi } from '../services/api'

type DesktopConnectButtonProps = {
  variant?: 'full' | 'icon' | 'inline'
}

const DMG_DOWNLOAD_URL = 'https://github.com/manishiitg/mcp-agent-builder-go/releases/download/v1.25.26/Runloop-1.25.26-arm64.dmg'
const DMG_INSTALL_COMMAND = 'curl -fsSL https://raw.githubusercontent.com/manishiitg/mcp-agent-builder-go/main/install.sh | bash'

function WorkspaceSwitcherHint() {
  return (
    <div className="flex items-center gap-3 rounded-md border border-gray-200 bg-gray-50 p-2 dark:border-slate-700 dark:bg-slate-950">
      <div className="flex h-28 w-14 flex-col items-center justify-end rounded-b-lg rounded-t-sm bg-[#303030] py-2 shadow-sm">
        <div className="mb-4 h-px w-9 bg-white/10" />
        <div className="mb-3 flex h-9 w-9 items-center justify-center rounded-md border border-emerald-400/60 bg-black/35 text-emerald-400">
          <MonitorDown className="h-4 w-4" />
        </div>
        <Bell className="mb-3 h-4 w-4 text-slate-300" />
        <div className="text-[9px] leading-none text-slate-300">v1.25.26</div>
      </div>
      <div className="min-w-0 text-xs leading-5 text-gray-600 dark:text-gray-300">
        Click the highlighted monitor icon at the bottom-left of the Mac app sidebar. That opens the workspace menu.
      </div>
    </div>
  )
}

export function DesktopConnectButton({ variant = 'full' }: DesktopConnectButtonProps) {
  const [status, setStatus] = useState<'idle' | 'loading' | 'copied' | 'error'>('idle')
  const [message, setMessage] = useState('')
  const [connectUrl, setConnectUrl] = useState('')
  const [showPopup, setShowPopup] = useState(false)
  const [installCopied, setInstallCopied] = useState(false)

  const copyConnectUrl = async (url = connectUrl) => {
    if (!url) return
    try {
      await navigator.clipboard.writeText(url)
      setStatus('copied')
      setMessage('Desktop connect URL copied.')
    } catch {
      setStatus('error')
      setMessage('Copy failed. Select and copy the URL manually.')
    }
  }

  const copyInstallCommand = async () => {
    await navigator.clipboard.writeText(DMG_INSTALL_COMMAND)
    setInstallCopied(true)
    setTimeout(() => setInstallCopied(false), 2000)
  }

  const handleCopy = async (openPopup = true) => {
    setStatus('loading')
    setMessage('')
    try {
      const response = await authApi.createDesktopConnect()
      setConnectUrl(response.connect_url)
      if (openPopup) setShowPopup(true)
      await copyConnectUrl(response.connect_url)
    } catch (error) {
      setStatus('error')
      setMessage(error instanceof Error ? error.message : 'Could not create desktop connect URL.')
      if (openPopup) setShowPopup(true)
    }
  }

  const popup = showPopup && (
    <div
      className="fixed inset-0 z-[100] flex items-center justify-center bg-black/35 p-4"
      onClick={() => setShowPopup(false)}
    >
      <div
        className="max-h-[85vh] w-[calc(100vw-2rem)] overflow-y-auto rounded-lg border border-gray-200 bg-white p-3 shadow-xl sm:w-[50vw] sm:max-w-2xl dark:border-slate-700 dark:bg-slate-900"
        onClick={(event) => event.stopPropagation()}
      >
        <div className="mb-3 flex items-start justify-between gap-3">
          <div className="flex items-center gap-2">
            <div className="flex h-8 w-8 items-center justify-center rounded-md bg-blue-50 text-blue-600 dark:bg-blue-950/40 dark:text-blue-300">
              <MonitorDown className="h-4 w-4" />
            </div>
            <div>
              <h3 className="text-sm font-semibold text-gray-900 dark:text-gray-100">Download app, then connect</h3>
              {message && (
                <p className={`text-xs ${status === 'error' ? 'text-red-600 dark:text-red-400' : 'text-emerald-600 dark:text-emerald-400'}`}>
                  {message}
                </p>
              )}
            </div>
          </div>
          <button
            type="button"
            className="rounded-md p-1 text-gray-400 hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-slate-800 dark:hover:text-gray-200"
            onClick={() => setShowPopup(false)}
            aria-label="Close"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <p className="mb-3 text-xs leading-relaxed text-gray-600 dark:text-gray-300">
          First install and open AgentWorks for Mac. In the app, use the bottom-left workspace switcher: click Local, choose Add workspace, paste this URL into Server URL, then Save.
        </p>

        <div className="mb-3 grid gap-2 text-xs text-gray-600 sm:grid-cols-2 dark:text-gray-300">
          <div className="rounded-md border border-gray-200 p-2 dark:border-slate-700">
            <div className="mb-1 font-semibold text-gray-900 dark:text-gray-100">1. Install Mac app</div>
            Use the DMG download or install command below.
          </div>
          <div className="rounded-md border border-gray-200 p-2 dark:border-slate-700">
            <div className="mb-1 font-semibold text-gray-900 dark:text-gray-100">2. Connect workspace</div>
            Bottom-left Local menu, Add workspace, Server URL, Save.
          </div>
        </div>
        <div className="mb-3">
          <WorkspaceSwitcherHint />
        </div>

        {connectUrl && (
          <div className="space-y-2">
            <label className="text-xs font-medium text-gray-600 dark:text-gray-300">Desktop connect URL</label>
            <div className="flex gap-2">
              <input
                readOnly
                value={connectUrl}
                className="min-w-0 flex-1 rounded-md border border-gray-200 bg-gray-50 px-2 py-2 text-xs text-gray-700 dark:border-slate-700 dark:bg-slate-950 dark:text-gray-200"
                onFocus={(event) => event.currentTarget.select()}
              />
              <Button type="button" variant="outline" size="sm" className="shrink-0 gap-2" onClick={() => copyConnectUrl()}>
                <Copy className="h-3.5 w-3.5" />
                Copy
              </Button>
            </div>
          </div>
        )}

        <div className="mt-3 space-y-2">
          <label className="text-xs font-medium text-gray-600 dark:text-gray-300">Install Mac app</label>
          <div className="rounded-md border border-gray-200 bg-gray-50 p-2 dark:border-slate-700 dark:bg-slate-950">
            <code className="block break-all text-[11px] leading-relaxed text-gray-700 dark:text-gray-200">{DMG_INSTALL_COMMAND}</code>
          </div>
          <Button type="button" variant="outline" size="sm" className="w-full gap-2" onClick={copyInstallCommand}>
            <Copy className="h-3.5 w-3.5" />
            {installCopied ? 'Command copied' : 'Copy install command'}
          </Button>
        </div>

        <div className="mt-3 flex flex-col gap-2 sm:flex-row sm:justify-end">
          <a
            href={DMG_DOWNLOAD_URL}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex h-9 items-center justify-center gap-2 rounded-md bg-blue-600 px-3 text-sm font-medium text-white hover:bg-blue-700"
          >
            Download DMG
            <ExternalLink className="h-3.5 w-3.5" />
          </a>
          <Button type="button" variant="outline" size="sm" onClick={() => setShowPopup(false)}>
            Done
          </Button>
        </div>
      </div>
    </div>
  )

  if (variant === 'icon') {
    return (
      <>
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              type="button"
              className={`p-2 transition-colors ${
                status === 'error'
                  ? 'text-red-500 hover:text-red-600 dark:text-red-400 dark:hover:text-red-300'
                  : 'text-blue-500 hover:text-blue-600 dark:text-blue-400 dark:hover:text-blue-300'
              }`}
              onClick={(event) => {
                event.stopPropagation()
                handleCopy()
              }}
              disabled={status === 'loading'}
              aria-label="Connect desktop app"
              title="Connect desktop app"
            >
              {status === 'loading' ? (
                <Loader2 className="h-5 w-5 animate-spin" />
              ) : (
                <MonitorDown className="h-5 w-5" />
              )}
            </button>
          </TooltipTrigger>
          <TooltipContent side="right">
            <p>Connect desktop app</p>
          </TooltipContent>
        </Tooltip>
        {popup}
      </>
    )
  }

  if (variant === 'inline') {
    return (
      <div className="space-y-4">
        <div className="rounded-lg border border-gray-200 p-4 dark:border-slate-700">
          <div className="mb-2 flex items-center gap-2 text-sm font-semibold text-gray-900 dark:text-gray-100">
            <Download className="h-4 w-4 text-blue-500" />
            1. Download and open the Mac app
          </div>
          <p className="mb-3 text-xs leading-5 text-gray-500 dark:text-gray-400">
            Install AgentWorks for Mac, open the app, then keep it running while you connect this workspace.
          </p>
          <div className="mb-3 rounded-md border border-gray-200 bg-gray-50 p-2 dark:border-slate-700 dark:bg-slate-950">
            <code className="block break-all text-[11px] leading-relaxed text-gray-700 dark:text-gray-200">{DMG_INSTALL_COMMAND}</code>
          </div>
          <div className="flex flex-col gap-2 sm:flex-row">
            <a
              href={DMG_DOWNLOAD_URL}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex h-9 items-center justify-center gap-2 rounded-md bg-blue-600 px-3 text-sm font-medium text-white hover:bg-blue-700"
            >
              Download DMG
              <ExternalLink className="h-3.5 w-3.5" />
            </a>
            <Button type="button" variant="outline" size="sm" className="gap-2" onClick={copyInstallCommand}>
              <Copy className="h-3.5 w-3.5" />
              {installCopied ? 'Command copied' : 'Copy install command'}
            </Button>
          </div>
        </div>

        <div className="rounded-lg border border-gray-200 p-4 dark:border-slate-700">
          <div className="mb-2 flex items-center gap-2 text-sm font-semibold text-gray-900 dark:text-gray-100">
            <MonitorDown className="h-4 w-4 text-blue-500" />
            2. Connect this workspace
          </div>
          <p className="mb-3 text-xs leading-5 text-gray-500 dark:text-gray-400">
            Generate a one-time URL. In the Mac app, click the bottom-left workspace switcher labeled Local, choose Add workspace, paste the URL into Server URL, then Save.
          </p>
          <div className="mb-3">
            <WorkspaceSwitcherHint />
          </div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="mb-3 w-full justify-center gap-2 sm:w-auto"
            onClick={() => handleCopy(false)}
            disabled={status === 'loading'}
          >
            {status === 'loading' ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <Copy className="h-4 w-4" />
            )}
            {connectUrl ? 'Create new connect URL' : 'Create and copy connect URL'}
          </Button>

          {connectUrl && (
            <div className="space-y-2">
              <label className="text-xs font-medium text-gray-600 dark:text-gray-300">Desktop connect URL</label>
              <div className="flex gap-2">
                <input
                  readOnly
                  value={connectUrl}
                  className="min-w-0 flex-1 rounded-md border border-gray-200 bg-gray-50 px-2 py-2 text-xs text-gray-700 dark:border-slate-700 dark:bg-slate-950 dark:text-gray-200"
                  onFocus={(event) => event.currentTarget.select()}
                />
                <Button type="button" variant="outline" size="sm" className="shrink-0 gap-2" onClick={() => copyConnectUrl()}>
                  <Copy className="h-3.5 w-3.5" />
                  Copy
                </Button>
              </div>
            </div>
          )}

          {message && (
            <div className={`mt-2 text-xs ${status === 'error' ? 'text-red-600 dark:text-red-400' : 'text-emerald-600 dark:text-emerald-400'}`}>
              {message}
            </div>
          )}
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-2">
      <Button
        type="button"
        variant="outline"
        size="sm"
        className="w-full justify-center gap-2"
        onClick={() => handleCopy()}
        disabled={status === 'loading'}
      >
        {status === 'loading' ? (
          <Loader2 className="h-4 w-4 animate-spin" />
        ) : status === 'copied' ? (
          <Copy className="h-4 w-4" />
        ) : (
          <MonitorDown className="h-4 w-4" />
        )}
        Connect desktop app
      </Button>
      {message && (
        <div className={`text-[11px] ${status === 'error' ? 'text-red-600 dark:text-red-400' : 'text-emerald-600 dark:text-emerald-400'}`}>
          {message}
        </div>
      )}
      {popup}
    </div>
  )
}
