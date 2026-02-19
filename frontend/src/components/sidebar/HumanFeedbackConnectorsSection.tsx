import { useState, useEffect } from 'react'
import { MessageSquare, ChevronDown, ChevronRight, CheckCircle, XCircle, Settings, Play, Bot } from 'lucide-react'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../ui/tooltip'
import { agentApi } from '../../services/api'
import type { SlackConfigResponse } from '../../services/api-types'
import SlackFeedbackConfig from '../settings/SlackFeedbackConfig'
import BotSimulatorModal from '../settings/BotSimulatorModal'
import BotConfigModal from '../settings/BotConfigModal'

interface HumanFeedbackConnectorsSectionProps {
  minimized?: boolean
}

export default function HumanFeedbackConnectorsSection({
  minimized = false,
}: HumanFeedbackConnectorsSectionProps) {
  const [isExpanded, setIsExpanded] = useState(true)
  const [showSlackConfig, setShowSlackConfig] = useState(false)
  const [showSimulator, setShowSimulator] = useState(false)
  const [showBotConfig, setShowBotConfig] = useState(false)
  const [slackConfig, setSlackConfig] = useState<SlackConfigResponse | null>(null)
  const [loading, setLoading] = useState(false)

  // Load configs when expanded
  useEffect(() => {
    if (isExpanded && !slackConfig && !loading) {
      loadSlackConfig()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isExpanded])

  const loadSlackConfig = async () => {
    try {
      setLoading(true)
      const data = await agentApi.getSlackFeedbackConfig()
      setSlackConfig(data)
    } catch (err) {
      console.error('Failed to load Slack config:', err)
    } finally {
      setLoading(false)
    }
  }

  const handleConfigClose = () => {
    setShowSlackConfig(false)
    loadSlackConfig()
  }

  if (minimized) {
    return (
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              onClick={(e) => {
                e.stopPropagation()
                setShowSlackConfig(true)
              }}
              className="p-2 text-muted-foreground hover:text-foreground transition-colors"
              title="Human Feedback Connectors"
            >
              <MessageSquare className="w-5 h-5" />
            </button>
          </TooltipTrigger>
          <TooltipContent>
            <p>Human Feedback Connectors</p>
          </TooltipContent>
        </Tooltip>
        <SlackFeedbackConfig
          isOpen={showSlackConfig}
          onClose={handleConfigClose}
        />
      </TooltipProvider>
    )
  }

  return (
    <TooltipProvider>
      <div>
        {/* Header */}
        <div className="flex items-center justify-between mb-2">
          <h3 className="text-sm font-semibold text-foreground flex items-center gap-2">
            <MessageSquare className="w-4 h-4" />
            Human Feedback Connectors
          </h3>
          <button
            onClick={() => setIsExpanded(!isExpanded)}
            className="text-muted-foreground hover:text-foreground transition-colors"
          >
            {isExpanded ? (
              <ChevronDown className="w-4 h-4" />
            ) : (
              <ChevronRight className="w-4 h-4" />
            )}
          </button>
        </div>

        {/* Content */}
        {isExpanded && (
          <div className="space-y-3">
            {/* Bot Configuration (global — applies to all bot interfaces) */}
            <div className="bg-card rounded-md p-3 space-y-2 border border-border">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <div className="w-8 h-8 rounded bg-green-100 dark:bg-green-900/30 flex items-center justify-center">
                    <Settings className="w-4 h-4 text-green-600 dark:text-green-400" />
                  </div>
                  <div>
                    <div className="text-sm font-medium text-foreground">Bot Configuration</div>
                    <div className="text-xs text-muted-foreground">Servers & skills for all bots</div>
                  </div>
                </div>
              </div>

              <button
                onClick={(e) => {
                  e.stopPropagation()
                  setShowBotConfig(true)
                }}
                className="w-full px-2 py-1.5 text-xs font-medium bg-secondary hover:bg-secondary/80 text-foreground rounded-md transition-colors flex items-center justify-center gap-1.5"
              >
                <Settings className="w-3 h-3" />
                Configure
              </button>
            </div>

            {/* Slack Connector */}
            <div className="bg-card rounded-md p-3 space-y-2 border border-border">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <div className="w-8 h-8 rounded bg-purple-100 dark:bg-purple-900/30 flex items-center justify-center">
                    <MessageSquare className="w-4 h-4 text-purple-600 dark:text-purple-400" />
                  </div>
                  <div>
                    <div className="text-sm font-medium text-foreground">Slack</div>
                    <div className="text-xs text-muted-foreground">Thread replies</div>
                  </div>
                </div>
                {loading ? (
                  <div className="text-xs text-muted-foreground">Loading...</div>
                ) : slackConfig ? (
                  <div className="flex items-center gap-1.5">
                    {slackConfig.enabled ? (
                      <>
                        <CheckCircle className="w-3.5 h-3.5 text-green-600 dark:text-green-400" />
                        <span className="text-xs font-medium text-green-600 dark:text-green-400">
                          Active
                        </span>
                      </>
                    ) : (
                      <>
                        <XCircle className="w-3.5 h-3.5 text-gray-400" />
                        <span className="text-xs font-medium text-gray-400">Inactive</span>
                      </>
                    )}
                  </div>
                ) : (
                  <span className="text-xs text-muted-foreground">Not configured</span>
                )}
              </div>

              {slackConfig?.enabled && slackConfig.channel_id && (
                <div className="text-xs text-muted-foreground truncate" title={slackConfig.channel_id}>
                  Channel: <span className="font-mono">{slackConfig.channel_id}</span>
                </div>
              )}

              <button
                onClick={(e) => {
                  e.stopPropagation()
                  setShowSlackConfig(true)
                }}
                className="w-full px-2 py-1.5 text-xs font-medium bg-secondary hover:bg-secondary/80 text-foreground rounded-md transition-colors flex items-center justify-center gap-1.5"
              >
                <Settings className="w-3 h-3" />
                Configure
              </button>
            </div>

            {/* Bot Simulator */}
            <div className="bg-card rounded-md p-3 space-y-2 border border-border">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <div className="w-8 h-8 rounded bg-blue-100 dark:bg-blue-900/30 flex items-center justify-center">
                    <Bot className="w-4 h-4 text-blue-600 dark:text-blue-400" />
                  </div>
                  <div>
                    <div className="text-sm font-medium text-foreground">Bot Simulator</div>
                    <div className="text-xs text-muted-foreground">Test bot flow</div>
                  </div>
                </div>
              </div>

              <button
                onClick={(e) => {
                  e.stopPropagation()
                  setShowSimulator(true)
                }}
                className="w-full px-2 py-1.5 text-xs font-medium bg-blue-600 hover:bg-blue-700 text-white rounded-md transition-colors flex items-center justify-center gap-1.5"
              >
                <Play className="w-3 h-3" />
                Simulate
              </button>
            </div>

            {/* Quick Info */}
            <div className="text-xs text-muted-foreground space-y-1 pt-2 border-t border-border">
              <div>• Configure servers & skills for all bot interfaces</div>
              <div>• Set up notification channels</div>
              <div>• Test bot flow with the simulator</div>
            </div>
          </div>
        )}

        {/* Slack Configuration Modal */}
        <SlackFeedbackConfig
          isOpen={showSlackConfig}
          onClose={handleConfigClose}
        />

        {/* Bot Simulator Modal */}
        <BotSimulatorModal
          isOpen={showSimulator}
          onClose={() => setShowSimulator(false)}
        />

        {/* Bot Config Modal */}
        <BotConfigModal
          isOpen={showBotConfig}
          onClose={() => setShowBotConfig(false)}
        />
      </div>
    </TooltipProvider>
  )
}
