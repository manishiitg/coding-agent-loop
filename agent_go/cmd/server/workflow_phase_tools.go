package server

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/guidance"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator"
	todo_creation_human "github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workflowtypes"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/mcpagent/llm"
)

// installWorkflowPhaseTools registers the phase-specific tool set on an agent
// (plan modification tools, workshop chat tools, evaluation tools, run_full_workflow,
// guidance/reference doc tools, etc.) and optionally applies a workshop-mode tool
// allow list.
//
// Extracted from the /api/query path in server.go (was the 358-line inline block
// inside the workflow-phase setup block). Two callers exist:
//
//  1. /api/query — passes the live request's options and applyAllowList=true so the
//     per-turn mode allow list narrows the visible tool set.
//  2. The chat-history auto-restore path (setupWorkflowPhaseToolsForRestore) —
//     passes a synthesized request built from the saved runtime + manifest and
//     applyAllowList=false so the restored CLI sees the superset; /api/query later
//     narrows it via SetToolAllowList when the first user turn arrives.
//
// Returns an error instead of fatal-exiting on RegisterPlanModificationTools
// failure. /api/query wraps the error with log.Fatalf to preserve the pre-refactor
// semantics; the restore path logs and skips so a partial-registration failure
// doesn't crash the server during a routine auto-restore.
func (api *StreamingAPI) installWorkflowPhaseTools(
	ctx context.Context,
	underlyingAgent *mcpagent.Agent,
	sessionID, userID, workflowPhaseID, phaseWorkspacePath, phaseRunFolder string,
	phaseTemplateVars map[string]string,
	selectedServers []string,
	mergedAPIKeys *llm.ProviderAPIKeys,
	phaseReadFile func(ctx context.Context, p string) (string, error),
	phaseWriteFile func(ctx context.Context, p, content string) error,
	phaseMoveFile func(ctx context.Context, src, dst string) error,
	syntheticReq QueryRequest,
	applyAllowList bool,
) error {
	// Register phase-appropriate tools
	// PHASE_TOOL_RACE_DIAGNOSTIC: these are the registrations that
	// the auto-restore path in chat_history_routes.go has NOT seen
	// yet — see [PHASE_TOOL_RACE] AUTO_RESTORE_LAUNCH log. If
	// AUTO_RESTORE_LAUNCH fired before PHASE_TOOL_REGISTER_START
	// for the same session, the CLI's tool catalog at launch is
	// missing run_full_workflow / execute_step / etc.
	log.Printf("[PHASE_TOOL_RACE] PHASE_TOOL_REGISTER_START for session=%s phase=%s",
		sessionID, workflowPhaseID)
	phaseRegisterStart := time.Now()
	switch workflowPhaseID {
	case workflowtypes.WorkflowStatusWorkflowBuilder:
		// Plan modification tools + workshop execution tools (execute_step, query_step, stop_step, etc.)
		// Returns an error on failure: the workflow-builder system prompt advertises these tools,
		// so a half-registered builder silently hallucinates missing tools to the LLM.
		// Schemas are covered by TestAllSchemaFunctionsReturnValidJSON — this should
		// never fire in a healthy build. The /api/query caller wraps this with log.Fatalf
		// to preserve the original Fatal semantics; the restore caller logs and skips.
		if err := todo_creation_human.RegisterPlanModificationTools(
			underlyingAgent,
			phaseWorkspacePath,
			api.logger,
			phaseReadFile,
			phaseWriteFile,
			phaseMoveFile,
			fmt.Sprintf("%s chat agent", workflowPhaseID),
		); err != nil {
			return fmt.Errorf("register plan modification tools for workflow-builder: %w", err)
		}
		log.Printf("[WORKFLOW_PHASE] Registered plan modification tools for %s", workflowPhaseID)
		if err := todo_creation_human.RegisterChiefOfStaffRecommendationStatusTool(
			underlyingAgent,
			phaseWorkspacePath,
			api.logger,
			phaseReadFile,
			phaseWriteFile,
		); err != nil {
			return fmt.Errorf("register Chief of Staff recommendation status tool for workflow-builder: %w", err)
		}
		log.Printf("[WORKFLOW_PHASE] Registered Chief of Staff recommendation status tool for %s", workflowPhaseID)

		// STOP-RACE GUARD: Check if the session was stopped while this goroutine
		// was in flight. Without this check, the goroutine would create a new
		// WorkshopChatSession with a fresh context.Background() that is never
		// canceled, leaving orphaned CLI processes running indefinitely.
		// This was the root cause of the 2026-04-04 "can't stop" bug.
		if api.isSessionMarkedStopped(sessionID) {
			log.Printf("[WORKFLOW_PHASE] Session %s was stopped — aborting workshop creation to prevent orphaned processes", sessionID)
			return nil
		}

		// Get or create per-session workshop controller + step registry
		workshopSessionKey := sessionID
		var workshopSession *todo_creation_human.WorkshopChatSession
		if cached, ok := api.workshopChatSessions.Load(workshopSessionKey); ok {
			workshopSession = cached.(*todo_creation_human.WorkshopChatSession)
			log.Printf("[WORKFLOW_PHASE] Reusing existing workshop session for %s", sessionID)

			// Always refresh API keys on session reuse (workspace keys may have changed)
			// Use mergedAPIKeys loaded before goroutine (r.Context() is canceled inside goroutine)
			workshopSession.UpdateAPIKeys(mergedAPIKeys)

			// Refresh enabled group IDs from current request (toolbar selection may have changed)
			if syntheticReq.ExecutionOptions != nil && len(syntheticReq.ExecutionOptions.EnabledGroupNames) > 0 {
				workshopSession.UpdateEnabledGroupNames(ctx, syntheticReq.ExecutionOptions.EnabledGroupNames)
				log.Printf("[WORKFLOW_PHASE] Refreshed enabled group names: %v", syntheticReq.ExecutionOptions.EnabledGroupNames)
			}

			// Pass frontend-selected workshop mode so AUTO-NOTIFICATION action hints use the correct mode
			if syntheticReq.ExecutionOptions != nil && syntheticReq.ExecutionOptions.WorkshopMode != "" {
				workshopSession.SetWorkshopModeOverride(syntheticReq.ExecutionOptions.WorkshopMode)
			}

			// Refresh all settings from manifest in case user edited the workflow
			if phaseWorkspacePath != "" {
				refreshManifest, refreshFound, refreshErr := ReadWorkflowManifest(context.Background(), phaseWorkspacePath)
				if refreshErr != nil {
					log.Printf("[WORKFLOW_PHASE] Warning: Failed to reload manifest: %v", refreshErr)
				} else if refreshFound {
					caps := refreshManifest.Capabilities
					selectedServers = caps.SelectedServers
					configuredBrowserMode := strings.ToLower(strings.TrimSpace(caps.BrowserMode))
					configuredBrowserPorts := configuredCDPPortsForMode(configuredBrowserMode, nil, caps.CDPPorts)
					workshopSession.UpdateBrowserRuntime(configuredBrowserMode, configuredBrowserPorts)
					common.SetSessionBrowserMode(sessionID, configuredBrowserMode)
					log.Printf("[WORKFLOW_PHASE] Refreshed dynamic browser config: configured_mode=%s candidate_cdp_ports=%v", configuredBrowserMode, configuredBrowserPorts)

					refreshedTools := caps.SelectedTools
					toolsParsed := true
					refreshedSkills := caps.SelectedSkills
					skillsParsed := true

					// Refresh secrets
					refreshedUserSecrets := api.loadSelectedSecrets(context.Background(), userID, phaseWorkspacePath, caps.SelectedSecrets)
					effectiveSecretSelection := syntheticReq.SelectedGlobalSecrets
					if caps.SelectedGlobalSecretNames != nil {
						effectiveSecretSelection = caps.SelectedGlobalSecretNames
					}
					allRefreshedSecrets := mergeGlobalSecrets(refreshedUserSecrets, effectiveSecretSelection)
					var secretEntries []orchestrator.SecretEntry
					for _, s := range allRefreshedSecrets {
						secretEntries = append(secretEntries, orchestrator.SecretEntry{Name: s.Name, Value: s.Value})
					}

					// LLM config
					refreshedKnowledgebase := true
					refreshedLockKnowledgebase := false
					log.Printf("[WORKFLOW_PHASE] Refresh LLMConfig: isNil=%v", caps.LLMConfig == nil)
					if caps.LLMConfig != nil {
						log.Printf("[WORKFLOW_PHASE] Refresh LLMConfig details: mode=%q tieredConfig=%v",
							caps.LLMConfig.Mode, caps.LLMConfig.TieredConfig != nil)
						phaseLLM, refreshedTiered := workshopResolveLLMConfig(caps.LLMConfig)
						maintenanceLLM := workshopResolveMaintenanceLLMConfig(caps.LLMConfig)
						workshopSession.UpdatePresetLLMConfigs(phaseLLM, maintenanceLLM)

						if refreshedTiered != nil {
							workshopSession.UpdateTieredConfig(refreshedTiered)
							log.Printf("[WORKFLOW_PHASE] Refreshed tiered config from manifest")
						} else {
							workshopSession.UpdateTieredConfig(nil)
						}

						if caps.LLMConfig.UseKnowledgebase != nil {
							refreshedKnowledgebase = *caps.LLMConfig.UseKnowledgebase
						}
						if caps.LLMConfig.LockKnowledgebase != nil {
							refreshedLockKnowledgebase = *caps.LLMConfig.LockKnowledgebase
						}
					}

					workshopSession.UpdatePresetSettings(
						selectedServers,
						refreshedTools, toolsParsed,
						caps.UseCodeExecutionMode,
						refreshedKnowledgebase,
						refreshedLockKnowledgebase,
						refreshedSkills, skillsParsed,
						secretEntries,
					)
					log.Printf("[WORKFLOW_PHASE] Refreshed settings from manifest: servers=%d tools=%d codeExec=%v kb=%v kbLock=%v skills=%d secrets=%d",
						len(selectedServers), len(refreshedTools), caps.UseCodeExecutionMode,
						refreshedKnowledgebase, refreshedLockKnowledgebase, len(refreshedSkills), len(secretEntries))
				}
			}
		} else {
			// Build full workshop config matching normal workflow setup
			workshopCfg, cfgErr := api.buildWorkshopConfig(ctx, syntheticReq, userID, phaseWorkspacePath, phaseRunFolder, selectedServers, sessionID, mergedAPIKeys)
			if cfgErr != nil {
				log.Printf("[WORKFLOW_PHASE] Error: Failed to build workshop config for %s: %v — workshop execution tools unavailable", workflowPhaseID, cfgErr)
			} else {
				newSession, sessionErr := todo_creation_human.NewWorkshopChatSession(ctx, workshopCfg)
				if sessionErr != nil {
					log.Printf("[WORKFLOW_PHASE] Warning: Failed to create workshop session for %s: %v — workshop execution tools unavailable", workflowPhaseID, sessionErr)
				} else {
					workshopSession = newSession
					api.workshopChatSessions.Store(workshopSessionKey, workshopSession)
					log.Printf("[WORKFLOW_PHASE] Created new %s session for %s", workflowPhaseID, sessionID)
				}
			}
		}

		if workshopSession != nil {
			workshopSession.SetExtraSubAgentNotifier(&workflowSubAgentTrackingNotifier{
				api:       api,
				sessionID: sessionID,
			})
			// The sub-agent tracker registers starts for status only and
			// notifies on completion. That gives the builder progress
			// updates for long-running orchestrator sub-agents without
			// synthetic turns at sub-agent start.
			//
			// Wire workshop execution notifier so execute_step and run_in_background
			// register in bgAgentRegistry (keeps frontend polling alive while background executions run).
			workshopSession.SetWorkshopExecutionNotifier(&workshopExecutionBgNotifier{
				api:           api,
				sessionID:     sessionID,
				workspacePath: phaseWorkspacePath,
				presetQueryID: syntheticReq.PresetQueryID,
				userID:        userID,
			})
			workshopSession.SetOnStepCorrelationDone(cleanupStepDelegation)
			workshopSession.SetExecutionStateChecks(
				func() bool {
					api.pendingMu.RLock()
					defer api.pendingMu.RUnlock()
					return len(api.pendingCompletions[sessionID]) > 0
				},
				func() bool { return api.bgAgentRegistry.HasRunningAgents(sessionID) },
				func() { api.cancelBackgroundAgents(sessionID) },
				func() []todo_creation_human.ServerAgentInfo {
					agents := api.bgAgentRegistry.GetAll(sessionID)
					result := make([]todo_creation_human.ServerAgentInfo, 0, len(agents))
					for _, a := range agents {
						result = append(result, todo_creation_human.ServerAgentInfo{
							ID: a.ID, Name: a.Name, Status: string(a.GetStatus()),
						})
					}
					return result
				},
			)
			todo_creation_human.RegisterWorkshopChatTools(underlyingAgent, workshopSession, api.logger)
			log.Printf("[WORKFLOW_PHASE] Registered workshop execution tools for %s (execute_step, query_step, stop_step, list_steps, etc.)", workflowPhaseID)

			builderSession := workshopSession
			afterUpsert := func(ctx context.Context, name, value string) error {
				if builderSession == nil {
					return nil
				}
				return builderSession.AttachSecretToWorkflow(ctx, name, value)
			}
			afterDelete := func(ctx context.Context, name string) error {
				if builderSession == nil {
					return nil
				}
				return builderSession.DetachSecretFromWorkflow(ctx, name)
			}
			if err := api.registerSecretManagementTools(underlyingAgent, userID, phaseWorkspacePath, "secret_tools", afterUpsert, afterDelete); err != nil {
				log.Printf("[WORKFLOW_PHASE] Warning: Failed to register secret tools in %s: %v", workflowPhaseID, err)
			} else {
				log.Printf("[WORKFLOW_PHASE] Registered secret tools in %s (list_secrets, set_workflow_secret, delete_workflow_secret, set_user_secret, delete_user_secret) with workflow auto-detach", workflowPhaseID)
			}
		}

		// Register evaluation tools in builder-style phases: validation plus
		// full execution against the current run.
		if err := todo_creation_human.RegisterEvaluationValidationTools(
			underlyingAgent,
			phaseWorkspacePath,
			api.logger,
			phaseReadFile,
			phaseWriteFile,
			phaseMoveFile,
		); err != nil {
			log.Printf("[WORKFLOW_PHASE] Warning: Failed to register evaluation validation tool in %s: %v", workflowPhaseID, err)
		} else {
			log.Printf("[WORKFLOW_PHASE] Registered evaluation validation tool in %s", workflowPhaseID)
		}

		if phaseTemplateVars["WorkshopMode"] == "workshop" || phaseTemplateVars["WorkshopMode"] == "builder" || phaseTemplateVars["WorkshopMode"] == "optimizer" || phaseTemplateVars["WorkshopMode"] == "reporting" {
			// Reporting tools: JSON report-plan read/write tools plus validation and
			// preview against real db/db.sqlite tables / knowledgebase sources. The renderer
			// silently drops bad widgets, so validation stays in the loop.
			if err := todo_creation_human.RegisterReportPlanManagementTools(
				underlyingAgent,
				phaseWorkspacePath,
				api.logger,
				phaseReadFile,
				phaseWriteFile,
			); err != nil {
				log.Printf("[WORKFLOW_PHASE] Warning: Failed to register report plan management tools in %s: %v", workflowPhaseID, err)
			} else {
				log.Printf("[WORKFLOW_PHASE] Registered report plan management tools in %s", workflowPhaseID)
			}

			if err := todo_creation_human.RegisterReportPlanValidationTools(
				underlyingAgent,
				phaseWorkspacePath,
				api.logger,
				phaseReadFile,
			); err != nil {
				log.Printf("[WORKFLOW_PHASE] Warning: Failed to register report plan validation tool in %s: %v", workflowPhaseID, err)
			} else {
				log.Printf("[WORKFLOW_PHASE] Registered report plan validation tool in %s", workflowPhaseID)
			}

			if err := todo_creation_human.RegisterReportRenderPreviewTool(
				underlyingAgent,
				phaseWorkspacePath,
				api.logger,
				phaseReadFile,
			); err != nil {
				log.Printf("[WORKFLOW_PHASE] Warning: Failed to register report render preview tool in %s: %v", workflowPhaseID, err)
			} else {
				log.Printf("[WORKFLOW_PHASE] Registered report render preview tool in %s", workflowPhaseID)
			}
		} else {
			log.Printf("[WORKFLOW_PHASE] Skipped report plan tools in %s mode for %s", phaseTemplateVars["WorkshopMode"], workflowPhaseID)
		}

		// Create eval session for run_full_evaluation (needs isEvaluationMode=true)
		evalSessionKey := "eval-" + sessionID
		var evalSession *todo_creation_human.WorkshopChatSession
		if cached, ok := api.workshopChatSessions.Load(evalSessionKey); ok {
			evalSession = cached.(*todo_creation_human.WorkshopChatSession)
			log.Printf("[WORKFLOW_PHASE] Reusing existing eval session in %s %s", workflowPhaseID, sessionID)
		} else {
			evalCfg, evalCfgErr := api.buildWorkshopConfig(ctx, syntheticReq, userID, phaseWorkspacePath, phaseRunFolder, selectedServers, sessionID, mergedAPIKeys)
			if evalCfgErr != nil {
				log.Printf("[WORKFLOW_PHASE] Error: Failed to build eval config in %s: %v", workflowPhaseID, evalCfgErr)
			} else {
				evalCfg.IsEvaluationMode = true
				newEvalSession, evalSessionErr := todo_creation_human.NewWorkshopChatSession(ctx, evalCfg)
				if evalSessionErr != nil {
					log.Printf("[WORKFLOW_PHASE] Warning: Failed to create eval session in %s: %v", workflowPhaseID, evalSessionErr)
				} else {
					evalSession = newEvalSession
					api.workshopChatSessions.Store(evalSessionKey, evalSession)
					log.Printf("[WORKFLOW_PHASE] Created eval session in %s for %s", workflowPhaseID, sessionID)
				}
			}
		}
		if evalSession != nil {
			evalSession.SetExtraSubAgentNotifier(&workflowSubAgentTrackingNotifier{
				api:       api,
				sessionID: sessionID,
			})
			evalSession.SetWorkshopExecutionNotifier(&workshopExecutionBgNotifier{
				api:           api,
				sessionID:     sessionID,
				workspacePath: phaseWorkspacePath,
				presetQueryID: syntheticReq.PresetQueryID,
				userID:        userID,
			})
			evalSession.SetExecutionStateChecks(
				func() bool {
					api.pendingMu.RLock()
					defer api.pendingMu.RUnlock()
					return len(api.pendingCompletions[sessionID]) > 0
				},
				func() bool { return api.bgAgentRegistry.HasRunningAgents(sessionID) },
				func() { api.cancelBackgroundAgents(sessionID) },
				func() []todo_creation_human.ServerAgentInfo {
					agents := api.bgAgentRegistry.GetAll(sessionID)
					result := make([]todo_creation_human.ServerAgentInfo, 0, len(agents))
					for _, a := range agents {
						result = append(result, todo_creation_human.ServerAgentInfo{
							ID: a.ID, Name: a.Name, Status: string(a.GetStatus()),
						})
					}
					return result
				},
			)
			todo_creation_human.RegisterRunFullEvaluationTool(underlyingAgent, evalSession, api.logger)
			log.Printf("[WORKFLOW_PHASE] Registered run_full_evaluation in %s", workflowPhaseID)
		}
		if workshopSession != nil {
			todo_creation_human.RegisterRunFullWorkflowTool(underlyingAgent, workshopSession, api.logger)
			log.Printf("[WORKFLOW_PHASE] Registered run_full_workflow in %s", workflowPhaseID)
			todo_creation_human.RegisterReorganizeKnowledgebaseTool(underlyingAgent, workshopSession, api.logger)
			log.Printf("[WORKFLOW_PHASE] Registered reorganize_knowledgebase in %s", workflowPhaseID)
			todo_creation_human.RegisterConsolidateKnowledgebaseTool(underlyingAgent, workshopSession, api.logger)
			log.Printf("[WORKFLOW_PHASE] Registered consolidate_knowledgebase in %s", workflowPhaseID)
			// Auto-improvement proposer tools stay in Workshop mode
			// (was Optimizer before the merge). capture_context is also
			// safe in Run mode because it requires explicit user
			// confirmation. Legacy "optimizer" is also accepted for
			// backward compat with persisted sessions that pre-date the
			// merge.
			switch phaseTemplateVars["WorkshopMode"] {
			case "workshop", "optimizer":
				RegisterAutoImprovementProposerTools(underlyingAgent, phaseWorkspacePath, "pulse-fixer", api.logger)
				log.Printf("[WORKFLOW_PHASE] Registered auto-improvement proposer tools in %s (mode=%s)", workflowPhaseID, phaseTemplateVars["WorkshopMode"])
			case "run":
				RegisterCaptureContextTool(underlyingAgent, phaseWorkspacePath, api.logger)
				log.Printf("[WORKFLOW_PHASE] Registered capture_context in %s (mode=%s)", workflowPhaseID, phaseTemplateVars["WorkshopMode"])
			default:
				log.Printf("[WORKFLOW_PHASE] Skipped auto-improvement proposer tools in %s (mode=%s)", workflowPhaseID, phaseTemplateVars["WorkshopMode"])
			}
			// Guided-flow text for every workflow slash command, returned via
			// get_workflow_command_guidance(kind=...). Available across modes;
			// per-kind mode validation lives in the tool itself.
			guidance.RegisterGuidanceTool(underlyingAgent, phaseTemplateVars["WorkshopMode"], api.logger)
			log.Printf("[WORKFLOW_PHASE] Registered get_workflow_command_guidance in %s (mode=%s)", workflowPhaseID, phaseTemplateVars["WorkshopMode"])

			// Reference docs for system-level content that used to live inline
			// in the workshop prompt (main.py rules, store contracts, message
			// sequence patterns, optimizer playbook, etc.) — returned via
			// get_reference_doc(kind=...). Same registry pattern as guidance;
			// per-kind mode validation lives in the tool itself.
			workshopMode := phaseTemplateVars["WorkshopMode"]
			guidance.RegisterReferenceDocTool(underlyingAgent, workshopMode, api.logger)
			log.Printf("[WORKFLOW_PHASE] Registered get_reference_doc in %s (mode=%s)", workflowPhaseID, workshopMode)

			// Attach the full reference surface: system-tools meta-skill
			// (advertises get_reference_doc and the precondition gates)
			// plus one materialized SKILL.md per reference doc / guided
			// flow. Both coexist — the static skills give each CLI a
			// browseable, file-mounted view via its native skill UI; the
			// meta-skill + tool path remains the authoritative way to
			// satisfy guidance precondition gates.
			guidance.AttachReferenceSurface(workshopMode, underlyingAgent.AttachSkill)
		}
	default:
		// planning: plan modification tools
		// Returns an error on failure — see workflow-builder case above for rationale.
		if err := todo_creation_human.RegisterPlanModificationTools(
			underlyingAgent,
			phaseWorkspacePath,
			api.logger,
			phaseReadFile,
			phaseWriteFile,
			phaseMoveFile,
			fmt.Sprintf("%s chat agent", workflowPhaseID),
		); err != nil {
			return fmt.Errorf("register plan modification tools for phase=%s: %w", workflowPhaseID, err)
		}
		log.Printf("[WORKFLOW_PHASE] Registered plan modification tools for phase=%s", workflowPhaseID)
	}

	log.Printf("[PHASE_TOOL_RACE] PHASE_TOOL_REGISTER_END for session=%s phase=%s elapsed=%s",
		sessionID, workflowPhaseID, time.Since(phaseRegisterStart))

	// Apply per-turn tool allow list based on current workshop mode.
	// This restricts which tools the LLM can see/call, enforcing mode boundaries
	// (e.g. DEBUG mode cannot execute steps, BUILD mode cannot optimize).
	// The allow list is applied in conversation.go (filteredTools) and buildToolIndex() (code exec).
	//
	// Skipped at restore time (applyAllowList=false): the auto-restore path
	// registers the superset so a later /api/query can narrow it once the user's
	// workshop mode is known. Narrowing at restore would lock the CLI to an
	// out-of-date mode (it could even be empty if the saved mode never made it
	// into the runtime).
	if applyAllowList {
		if workflowPhaseID == workflowtypes.WorkflowStatusWorkflowBuilder {
			workshopMode := phaseTemplateVars["WorkshopMode"]
			allowedTools := todo_creation_human.GetToolsForWorkshopMode(workshopMode)
			underlyingAgent.SetToolAllowList(allowedTools)
			log.Printf("[WORKSHOP_TOOLS] Applied tool allow list for mode=%s (%d tools): %v", workshopMode, len(allowedTools), allowedTools)
		} else {
			// Non-workshop phases get all tools
			underlyingAgent.ClearToolAllowList()
		}
	}

	// Rebuild code execution registry after prompt + tool changes
	if err := underlyingAgent.UpdateCodeExecutionRegistry(); err != nil {
		log.Printf("[WORKFLOW_PHASE] Warning: Failed to update code execution registry: %v", err)
	}

	return nil
}
