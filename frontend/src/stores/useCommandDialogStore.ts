import { create } from 'zustand'

type DialogName = 'skillImport' | 'mcpDetails' | 'mcpConfig' | 'models' | 'delegationTiers' | 'presetSettings'

interface CommandDialogState {
  showSkillImport: boolean
  showMCPDetails: boolean
  showMCPConfig: boolean
  showModels: boolean
  showDelegationTiers: boolean
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
  delegationTiers: 'showDelegationTiers',
  presetSettings: 'showPresetSettings',
}

export const useCommandDialogStore = create<CommandDialogState>()((set) => ({
  showSkillImport: false,
  showMCPDetails: false,
  showMCPConfig: false,
  showModels: false,
  showDelegationTiers: false,
  showPresetSettings: false,
  openDialog: (dialog) => set({ [dialogKeyMap[dialog]]: true }),
  closeDialog: (dialog) => set({ [dialogKeyMap[dialog]]: false }),
  closeAll: () => set({
    showSkillImport: false,
    showMCPDetails: false,
    showMCPConfig: false,
    showModels: false,
    showDelegationTiers: false,
    showPresetSettings: false,
  }),
}))
