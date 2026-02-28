import { create } from 'zustand'

type DialogName = 'skillImport' | 'mcpDetails' | 'mcpConfig' | 'models' | 'resume' | 'delegationTiers' | 'workflowBuilder' | 'presetSettings' | 'subAgentImport'

interface CommandDialogState {
  showSkillImport: boolean
  showMCPDetails: boolean
  showMCPConfig: boolean
  showModels: boolean
  showResume: boolean
  showDelegationTiers: boolean
  showWorkflowBuilder: boolean
  showPresetSettings: boolean
  showSubAgentImport: boolean
  openDialog: (dialog: DialogName) => void
  closeDialog: (dialog: DialogName) => void
  closeAll: () => void
}

const dialogKeyMap: Record<DialogName, keyof CommandDialogState> = {
  skillImport: 'showSkillImport',
  mcpDetails: 'showMCPDetails',
  mcpConfig: 'showMCPConfig',
  models: 'showModels',
  resume: 'showResume',
  delegationTiers: 'showDelegationTiers',
  workflowBuilder: 'showWorkflowBuilder',
  presetSettings: 'showPresetSettings',
  subAgentImport: 'showSubAgentImport',
}

export const useCommandDialogStore = create<CommandDialogState>()((set) => ({
  showSkillImport: false,
  showMCPDetails: false,
  showMCPConfig: false,
  showModels: false,
  showResume: false,
  showDelegationTiers: false,
  showWorkflowBuilder: false,
  showPresetSettings: false,
  showSubAgentImport: false,
  openDialog: (dialog) => set({ [dialogKeyMap[dialog]]: true }),
  closeDialog: (dialog) => set({ [dialogKeyMap[dialog]]: false }),
  closeAll: () => set({
    showSkillImport: false,
    showMCPDetails: false,
    showMCPConfig: false,
    showModels: false,
    showResume: false,
    showDelegationTiers: false,
    showWorkflowBuilder: false,
    showPresetSettings: false,
    showSubAgentImport: false,
  }),
}))
