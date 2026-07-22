import { create } from 'zustand'
import { resolveSetState, type SetStateAction } from './storeUtils'

// The child's shared identity/profile — read by both Parent Mode and Child
// Mode screens.
interface FamilyState {
  childName: string
  setChildName: (v: SetStateAction<string>) => void
  grade: string
  setGrade: (v: SetStateAction<string>) => void
  board: string
  setBoard: (v: SetStateAction<string>) => void
  // How the parent wants to be referred to when Quill talks ABOUT them to the
  // child ("mom", "dad", "grandma", a first name). Empty until Quill asks/learns it.
  parentLabel: string
  setParentLabel: (v: SetStateAction<string>) => void
}

export const useFamilyStore = create<FamilyState>()((set) => ({
  childName: 'Maya',
  setChildName: (v) => set((s) => ({ childName: resolveSetState(v, s.childName) })),
  grade: '10',
  setGrade: (v) => set((s) => ({ grade: resolveSetState(v, s.grade) })),
  board: 'CBSE',
  setBoard: (v) => set((s) => ({ board: resolveSetState(v, s.board) })),
  parentLabel: '',
  setParentLabel: (v) => set((s) => ({ parentLabel: resolveSetState(v, s.parentLabel) })),
}))
