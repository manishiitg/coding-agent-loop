import React, { useState } from 'react'
import { X, Eye, EyeOff, ImagePlus, CheckCircle, XCircle, Loader2 } from 'lucide-react'
import { useImageGenStore } from '../stores/useImageGenStore'
import { agentApi } from '../services/api'

interface ImageGenerationConfigModalProps {
  onClose: () => void
  onDisable?: () => void
}

const IMAGE_GEN_MODELS: Record<string, { id: string; label: string; cost: string }[]> = {
  vertex: [
    { id: 'gemini-3.1-flash-image-preview', label: 'Nano Banana 2 (Gemini 3.1 Flash Image)', cost: '$0.045/0.5K · $0.067/1K · $0.101/2K · $0.151/4K' },
    { id: 'gemini-3-pro-image-preview', label: 'Nano Banana Pro (Gemini 3 Pro Image)', cost: '$0.134/1K-2K · $0.24/4K' },
    { id: 'gemini-2.5-flash-image', label: 'Nano Banana (Gemini 2.5 Flash Image)', cost: '$0.039/image' },
  ],
  minimax: [
    { id: 'image-01', label: 'MiniMax Image-01', cost: '$0.0035/image' },
  ],
}

export const ImageGenerationConfigModal: React.FC<ImageGenerationConfigModalProps> = ({ onClose, onDisable }) => {
  const { config, setConfig } = useImageGenStore()
  const [showApiKey, setShowApiKey] = useState(false)
  const [localConfig, setLocalConfig] = useState({ ...config })
  const [testState, setTestState] = useState<'idle' | 'loading' | 'ok' | 'error'>('idle')
  const [testMessage, setTestMessage] = useState('')
  const [testImageSrc, setTestImageSrc] = useState<string | null>(null)

  const handleSave = () => {
    setConfig(localConfig)
    onClose()
  }

  const handleDisable = () => {
    setConfig(localConfig)
    onDisable?.()
    onClose()
  }

  const handleTest = async () => {
    setTestState('loading')
    setTestMessage('')
    setTestImageSrc(null)
    try {
      const result = await agentApi.testImageGen({
        provider: localConfig.provider,
        model_id: localConfig.modelId,
        api_key: localConfig.apiKey || undefined,
      })
      if (result.valid) {
        setTestState('ok')
        setTestMessage(result.message || 'Image generation is working')
        if (result.image_data) {
          setTestImageSrc(`data:image/png;base64,${result.image_data}`)
        } else if (result.image_url) {
          setTestImageSrc(result.image_url)
        }
      } else {
        setTestState('error')
        setTestMessage(result.error || 'Test failed')
      }
    } catch (e) {
      setTestState('error')
      setTestMessage(e instanceof Error ? e.message : 'Test failed')
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm"
      onClick={onClose}
    >
      <div
        className="bg-gray-900 rounded-xl shadow-2xl border border-gray-700 w-[440px] max-w-[90vw]"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-gray-700">
          <div className="flex items-center gap-2">
            <ImagePlus className="w-5 h-5 text-purple-400" />
            <h3 className="text-base font-semibold text-white">Image Generation Config</h3>
          </div>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-200 transition-colors">
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Body */}
        <div className="px-5 py-4 space-y-4">
          {/* Provider */}
          <div>
            <label className="block text-xs font-medium text-gray-400 mb-1.5">Provider</label>
            <select
              value={localConfig.provider}
              onChange={(e) => {
                const p = e.target.value
                const models = IMAGE_GEN_MODELS[p] ?? []
                setLocalConfig({ ...localConfig, provider: p, modelId: models[0]?.id ?? '' })
              }}
              className="w-full bg-gray-800 border border-gray-600 text-white text-sm rounded-md px-3 py-2 focus:outline-none focus:ring-1 focus:ring-purple-500"
            >
              <option value="vertex">Vertex AI (Imagen)</option>
              <option value="minimax">MiniMax</option>
            </select>
          </div>

          {/* Model */}
          <div>
            <label className="block text-xs font-medium text-gray-400 mb-1.5">Model</label>
            <select
              value={localConfig.modelId}
              onChange={(e) => setLocalConfig({ ...localConfig, modelId: e.target.value })}
              className="w-full bg-gray-800 border border-gray-600 text-white text-sm rounded-md px-3 py-2 focus:outline-none focus:ring-1 focus:ring-purple-500"
            >
              {(IMAGE_GEN_MODELS[localConfig.provider] ?? []).map((m) => (
                <option key={m.id} value={m.id}>
                  {m.label} — {m.cost}
                </option>
              ))}
            </select>
          </div>

          {/* API Key */}
          <div>
            <label className="block text-xs font-medium text-gray-400 mb-1.5">
              {localConfig.provider === 'minimax' ? 'MINIMAX_API_KEY' : 'GEMINI_API_KEY'}
              <span className="text-gray-500 font-normal ml-1">(optional if env var is set on server)</span>
            </label>
            <div className="relative">
              <input
                type={showApiKey ? 'text' : 'password'}
                value={localConfig.apiKey}
                onChange={(e) => { setLocalConfig({ ...localConfig, apiKey: e.target.value }); setTestState('idle'); setTestImageSrc(null) }}
                placeholder={localConfig.provider === 'minimax' ? 'sk-api-...' : 'AIza...'}
                className="w-full bg-gray-800 border border-gray-600 text-white text-sm rounded-md px-3 py-2 pr-10 focus:outline-none focus:ring-1 focus:ring-purple-500 placeholder-gray-600"
              />
              <button
                type="button"
                onClick={() => setShowApiKey(!showApiKey)}
                className="absolute right-2 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-200"
              >
                {showApiKey ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
              </button>
            </div>
            <p className="text-xs text-gray-500 mt-1">
              {localConfig.provider === 'minimax'
                ? 'If blank, the server uses MINIMAX_API_KEY environment variable.'
                : 'If blank, the server uses GEMINI_API_KEY or GOOGLE_API_KEY environment variables.'}
            </p>
          </div>

          {/* Test result */}
          {testState !== 'idle' && (
            <div className={`flex items-start gap-2 text-xs rounded-md px-3 py-2 ${
              testState === 'loading' ? 'bg-gray-800 text-gray-400' :
              testState === 'ok' ? 'bg-green-900/30 text-green-400 border border-green-800' :
              'bg-red-900/30 text-red-400 border border-red-800'
            }`}>
              {testState === 'loading' && <Loader2 className="w-3.5 h-3.5 mt-0.5 animate-spin flex-shrink-0" />}
              {testState === 'ok' && <CheckCircle className="w-3.5 h-3.5 mt-0.5 flex-shrink-0" />}
              {testState === 'error' && <XCircle className="w-3.5 h-3.5 mt-0.5 flex-shrink-0" />}
              <span>{testState === 'loading' ? 'Testing… generating a sample image' : testMessage}</span>
            </div>
          )}

          {/* Generated test image */}
          {testImageSrc && (
            <div className="rounded-md overflow-hidden border border-gray-700">
              <img src={testImageSrc} alt="Test generated image" className="w-full object-contain max-h-48" />
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between px-5 py-4 border-t border-gray-700">
          <div className="flex gap-2">
            <button
              onClick={handleTest}
              disabled={testState === 'loading'}
              className="px-3 py-1.5 text-sm bg-gray-700 hover:bg-gray-600 disabled:opacity-50 text-white rounded-md transition-colors flex items-center gap-1.5"
            >
              {testState === 'loading' && <Loader2 className="w-3.5 h-3.5 animate-spin" />}
              Test
            </button>
            {onDisable && (
              <button
                onClick={handleDisable}
                className="px-3 py-1.5 text-sm text-red-400 hover:text-red-300 hover:bg-red-900/20 rounded-md transition-colors"
              >
                Disable
              </button>
            )}
          </div>
          <div className="flex gap-2">
            <button onClick={onClose} className="px-4 py-1.5 text-sm text-gray-400 hover:text-gray-200 transition-colors">
              Cancel
            </button>
            <button
              onClick={handleSave}
              className="px-4 py-1.5 text-sm bg-purple-600 hover:bg-purple-500 text-white rounded-md transition-colors"
            >
              Save
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
