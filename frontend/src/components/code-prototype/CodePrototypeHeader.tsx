import React, { useState, useEffect, useRef, useCallback } from 'react'
import {
  Code, ChevronDown, Rocket, ExternalLink, Loader2,
  Monitor, Terminal, SquarePen, StopCircle, Save, History, Upload, FlaskConical,
  Check, X,
} from 'lucide-react'
import { useCodePrototypeStore } from '../../stores/useCodePrototypeStore'
import { useAppStore } from '../../stores/useAppStore'
import { codePrototypeApi } from '../../api/codePrototype'
import type { GitHubStatus } from '../../api/codePrototype'
import { ProjectListModal } from './ProjectListModal'
import { SaveCheckpointModal } from './SaveCheckpointModal'
import { PublishModal } from './PublishModal'
import { GitHubVersionsDropdown } from './GitHubVersionsDropdown'

export const CodePrototypeHeader: React.FC<{ onNewChat: () => void }> = ({ onNewChat }) => {
  const {
    currentProject,
    setCurrentProject,
    isDeploying,
    lastDeployedUrl,
    selectedProvider,
    showPreview,
    showLogs,
    setSelectedProvider,
    setDeploying,
    appendDeployLog,
    clearDeployLog,
    setLastDeployedUrl,
    setShowPreview,
    setShowLogs,
  } = useCodePrototypeStore()

  const { setWorkspaceMinimized } = useAppStore()

  const [showProjectList, setShowProjectList] = useState(false)
  const [isStopping, setIsStopping] = useState(false)

  const [gitStatus, setGitStatus] = useState<GitHubStatus | null>(null)
  const [showSave, setShowSave] = useState(false)
  const [showPublish, setShowPublish] = useState(false)
  const [showVersions, setShowVersions] = useState(false)
  const [isSaving, setIsSaving] = useState(false)
  const [saveFeedback, setSaveFeedback] = useState<'saved' | 'nothing' | null>(null)
  const [isKeeping, setIsKeeping] = useState(false)
  const [isDiscarding, setIsDiscarding] = useState(false)
  const [confirmExperiment, setConfirmExperiment] = useState<'keep' | 'discard' | null>(null)
  const versionsRef = useRef<HTMLDivElement>(null)

  const fetchGitStatus = useCallback(async () => {
    if (!currentProject) return
    try {
      const status = await codePrototypeApi.githubStatus(currentProject.name)
      setGitStatus(status)
    } catch {
      setGitStatus(null)
    }
  }, [currentProject])

  useEffect(() => {
    setGitStatus(null)
    fetchGitStatus()
  }, [fetchGitStatus])

  const handleStopDevServers = async () => {
    if (isStopping) return
    setIsStopping(true)
    try { await codePrototypeApi.stopDevServers() }
    catch (e) { console.error('[CODE-PROTOTYPE] stop error:', e) }
    finally { setIsStopping(false) }
  }

  const handleDeploy = async () => {
    if (!currentProject || isDeploying) return
    clearDeployLog()
    setDeploying(true)
    setLastDeployedUrl(null)
    appendDeployLog(`▶ Deploying "${currentProject.name}" to ${selectedProvider}…`)
    try {
      const result = await codePrototypeApi.deploy({ project_name: currentProject.name, provider: selectedProvider })
      result.logs.split('\n').filter(Boolean).forEach(l => appendDeployLog(l))
      appendDeployLog(`✓ Deployed: ${result.url}`)
      setLastDeployedUrl(result.url)
    } catch (e: unknown) {
      appendDeployLog(`✗ Deploy failed: ${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setDeploying(false)
    }
  }

  const handleQuickSave = async () => {
    if (!currentProject || isSaving) return
    setIsSaving(true)
    setSaveFeedback(null)
    try {
      const result = await codePrototypeApi.githubSaveCheckpoint(currentProject.name, 'Checkpoint')
      setSaveFeedback(result.message === 'nothing to save' ? 'nothing' : 'saved')
      setTimeout(() => setSaveFeedback(null), 2500)
    } catch {
      setShowSave(true)
    } finally {
      setIsSaving(false)
    }
  }

  const handleKeepExperiment = async () => {
    if (!currentProject || isKeeping) return
    setIsKeeping(true)
    try { await codePrototypeApi.githubKeepExperiment(currentProject.name); await fetchGitStatus() }
    catch (e) { console.error('[GITHUB] keep error:', e) }
    finally { setIsKeeping(false) }
  }

  const handleDiscardExperiment = async () => {
    if (!currentProject || isDiscarding) return
    setIsDiscarding(true)
    try { await codePrototypeApi.githubDiscardExperiment(currentProject.name); await fetchGitStatus() }
    catch (e) { console.error('[GITHUB] discard error:', e) }
    finally { setIsDiscarding(false) }
  }

  const handleConnected = async () => {
    if (!currentProject) return
    try { setCurrentProject(await codePrototypeApi.getProject(currentProject.name)) } catch {}
    await fetchGitStatus()
    setShowPublish(false)
    setShowPublish(true)
  }

  const handleDisconnected = async () => {
    if (!currentProject) return
    try { setCurrentProject(await codePrototypeApi.getProject(currentProject.name)) } catch {}
    setGitStatus(null)
  }

  const isGitConnected = gitStatus?.connected === true
  const isOnExperiment = isGitConnected && gitStatus?.is_experiment === true

  // Shared icon-button style
  const iconBtn = 'p-1.5 text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 transition-colors disabled:opacity-40'

  return (
    <>
      <div className="flex items-center gap-1.5 px-3 py-1.5 border-b border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800 flex-shrink-0">

        {/* ── Left: project identity ─────────────────────────────── */}
        <Code className="w-4 h-4 text-emerald-500 flex-shrink-0" />

        <button
          onClick={() => setShowProjectList(true)}
          className="flex items-center gap-1 text-sm font-medium text-gray-900 dark:text-gray-100 hover:text-emerald-600 dark:hover:text-emerald-400 transition-colors"
        >
          {currentProject?.name ?? 'Select project'}
          <ChevronDown className="w-3 h-3 text-gray-400" />
        </button>

        <button onClick={onNewChat} title="New chat" className={iconBtn}>
          <SquarePen className="w-4 h-4" />
        </button>

        {/* ── Experiment banner (conditional) ───────────────────── */}
        {isOnExperiment && (
          <>
            <div className="w-px h-4 bg-gray-200 dark:bg-gray-600 mx-0.5" />
            <div className="flex items-center gap-1 px-2 py-1 bg-violet-50 dark:bg-violet-950/40 border border-violet-200 dark:border-violet-800/50 rounded-md">
              <FlaskConical className="w-3.5 h-3.5 text-violet-500 dark:text-violet-600 flex-shrink-0" />
              <span className="text-xs text-violet-700 dark:text-violet-500 font-medium max-w-[120px] truncate">
                {gitStatus?.experiment_label ?? 'experiment'}
              </span>
              <button
                onClick={() => setConfirmExperiment('keep')}
                disabled={isKeeping || isDiscarding}
                title="Keep this experiment"
                className="p-0.5 rounded text-violet-400 dark:text-violet-600 hover:text-violet-600 dark:hover:text-violet-400 hover:bg-violet-100 dark:hover:bg-violet-900/40 disabled:opacity-40 transition-colors"
              >
                {isKeeping ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Check className="w-3.5 h-3.5" />}
              </button>
              <button
                onClick={() => setConfirmExperiment('discard')}
                disabled={isKeeping || isDiscarding}
                title="Discard this experiment"
                className="p-0.5 rounded text-violet-400 dark:text-violet-600 hover:text-violet-600 dark:hover:text-violet-400 hover:bg-violet-100 dark:hover:bg-violet-900/40 disabled:opacity-40 transition-colors"
              >
                {isDiscarding ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <X className="w-3.5 h-3.5" />}
              </button>
            </div>
          </>
        )}

        <div className="flex-1" />

        {/* ── Preview ───────────────────────────────────────────── */}
        <button
          onClick={() => { const next = !showPreview; setShowPreview(next); if (next) setWorkspaceMinimized(true) }}
          title="Toggle preview"
          className={`flex items-center gap-1 px-2 py-1 text-xs rounded-md border transition-colors ${
            showPreview
              ? 'bg-emerald-50 dark:bg-emerald-900/30 border-emerald-400 dark:border-emerald-600 text-emerald-700 dark:text-emerald-300'
              : 'border-gray-300 dark:border-gray-600 text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200'
          }`}
        >
          <Monitor className="w-3.5 h-3.5" />
          <span className="font-medium">Preview</span>
        </button>

        {/* ── Logs ──────────────────────────────────────────────── */}
        <button
          onClick={() => { const next = !showLogs; setShowLogs(next); if (next) setWorkspaceMinimized(true) }}
          title="Toggle container logs"
          className={`flex items-center gap-1 px-2 py-1 text-xs rounded-md border transition-colors ${
            showLogs
              ? 'bg-gray-800 dark:bg-gray-900 border-gray-500 dark:border-gray-600 text-gray-100 dark:text-gray-200'
              : 'border-gray-300 dark:border-gray-600 text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200'
          }`}
        >
          <Terminal className="w-3.5 h-3.5" />
          <span className="font-medium">Logs</span>
        </button>

        {/* ── GitHub: Save / Versions / Publish (icon-only) ─────── */}
        {currentProject && (
          <>
            <div className="w-px h-4 bg-gray-200 dark:bg-gray-600 mx-0.5" />

            {/* Save */}
            <button
              onClick={isGitConnected ? handleQuickSave : () => setShowSave(true)}
              disabled={isSaving}
              title={saveFeedback === 'saved' ? 'Saved ✓' : saveFeedback === 'nothing' ? 'Nothing to save' : 'Save checkpoint'}
              className={`${iconBtn} ${saveFeedback === 'saved' ? 'text-emerald-500 dark:text-emerald-400' : ''}`}
            >
              {isSaving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Save className="w-4 h-4" />}
            </button>

            {/* Versions */}
            <div className="relative" ref={versionsRef}>
              <button
                onClick={() => setShowVersions(v => !v)}
                title="Version history"
                className={`${iconBtn} flex items-center gap-0.5`}
              >
                <History className="w-4 h-4" />
                <ChevronDown className="w-2.5 h-2.5" />
              </button>
              {showVersions && (
                <GitHubVersionsDropdown
                  projectName={currentProject.name}
                  isConnected={isGitConnected}
                  onClose={() => setShowVersions(false)}
                  onStatusChange={fetchGitStatus}
                />
              )}
            </div>

            {/* Publish */}
            <button
              onClick={() => setShowPublish(true)}
              title={isGitConnected ? 'Publish to GitHub' : 'Connect GitHub'}
              className={`${iconBtn} ${isGitConnected ? '' : 'text-gray-300 dark:text-gray-600'}`}
            >
              <Upload className="w-4 h-4" />
            </button>
          </>
        )}

        {/* ── Deploy ────────────────────────────────────────────── */}
        <div className="w-px h-4 bg-gray-200 dark:bg-gray-600 mx-0.5" />

        <select
          value={selectedProvider}
          onChange={e => setSelectedProvider(e.target.value as 'k8s' | 'vercel' | 'railway')}
          className="text-xs border border-gray-300 dark:border-gray-600 rounded-md px-1.5 py-1 bg-white dark:bg-gray-700 text-gray-600 dark:text-gray-300"
        >
          <option value="k8s">k8s</option>
          <option value="vercel">Vercel</option>
          <option value="railway">Railway</option>
        </select>

        <button
          onClick={handleDeploy}
          disabled={!currentProject || isDeploying}
          className="flex items-center gap-1 px-2.5 py-1 text-xs font-medium bg-emerald-600 hover:bg-emerald-700 text-white rounded-md disabled:opacity-50 transition-colors"
        >
          {isDeploying ? <Loader2 className="w-3 h-3 animate-spin" /> : <Rocket className="w-3 h-3" />}
          {isDeploying ? 'Deploying…' : 'Deploy'}
        </button>

        {lastDeployedUrl && (
          <a href={lastDeployedUrl} target="_blank" rel="noreferrer" title={lastDeployedUrl} className={iconBtn}>
            <ExternalLink className="w-4 h-4 text-emerald-500" />
          </a>
        )}

        {/* ── Secondary: config + stop (icon-only, far right) ───── */}
        <div className="w-px h-4 bg-gray-200 dark:bg-gray-600 mx-0.5" />

        <button
          onClick={handleStopDevServers}
          disabled={isStopping}
          title="Stop all dev servers"
          className={`${iconBtn} hover:text-red-500`}
        >
          {isStopping ? <Loader2 className="w-4 h-4 animate-spin" /> : <StopCircle className="w-4 h-4" />}
        </button>
      </div>

      {/* Modals */}
      {showProjectList && <ProjectListModal onClose={() => setShowProjectList(false)} />}

      {confirmExperiment && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
          <div className="bg-white dark:bg-gray-800 rounded-xl shadow-xl w-full max-w-sm mx-4 p-6">
            <div className="flex items-center gap-2 mb-3">
              <FlaskConical className="w-5 h-5 text-violet-500" />
              <h3 className="text-base font-semibold text-gray-900 dark:text-gray-100">
                {confirmExperiment === 'keep' ? 'Keep experiment?' : 'Discard experiment?'}
              </h3>
            </div>
            <p className="text-sm text-gray-600 dark:text-gray-400 mb-1">
              <span className="font-medium text-gray-800 dark:text-gray-200">
                {gitStatus?.experiment_label ?? 'experiment'}
              </span>
            </p>
            <p className="text-xs text-gray-500 dark:text-gray-400 mt-2">
              {confirmExperiment === 'keep'
                ? 'This will merge your experiment into the main version. The experiment branch will be removed.'
                : 'This will permanently discard all changes in this experiment and return to the main version.'}
            </p>
            <div className="flex justify-end gap-2 mt-5">
              <button
                onClick={() => setConfirmExperiment(null)}
                className="px-4 py-2 text-sm text-gray-600 dark:text-gray-300 hover:text-gray-900 dark:hover:text-gray-100 transition-colors"
              >
                Cancel
              </button>
              <button
                onClick={() => {
                  setConfirmExperiment(null)
                  if (confirmExperiment === 'keep') handleKeepExperiment()
                  else handleDiscardExperiment()
                }}
                className={`px-4 py-2 text-sm font-medium text-white rounded-lg transition-colors ${
                  confirmExperiment === 'keep'
                    ? 'bg-violet-600 hover:bg-violet-700'
                    : 'bg-red-500 hover:bg-red-600'
                }`}
              >
                {confirmExperiment === 'keep' ? 'Keep it' : 'Discard'}
              </button>
            </div>
          </div>
        </div>
      )}

      {showSave && currentProject && (
        <SaveCheckpointModal
          projectName={currentProject.name}
          defaultMessage=""
          onClose={() => setShowSave(false)}
          onSaved={() => { setSaveFeedback('saved'); setTimeout(() => setSaveFeedback(null), 2500) }}
        />
      )}

      {showPublish && currentProject && (
        <PublishModal
          projectName={currentProject.name}
          github={currentProject.github}
          onClose={() => setShowPublish(false)}
          onConnected={handleConnected}
          onDisconnected={handleDisconnected}
        />
      )}
    </>
  )
}
