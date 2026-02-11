import { create } from 'zustand'

type DialogName = 'skillImport' | 'mcpDetails' | 'mcpConfig' | 'models' | 'resume' | 'delegationTiers' | 'workflowBuilder' | 'presetSettings'

interface CommandDialogState {
  showSkillImport: boolean
  showMCPDetails: boolean
  showMCPConfig: boolean
  showModels: boolean
  showResume: boolean
  showDelegationTiers: boolean
  showWorkflowBuilder: boolean
  showPresetSettings: boolean
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
  }),
}))
