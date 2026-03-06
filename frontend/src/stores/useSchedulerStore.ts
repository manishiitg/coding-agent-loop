import { create } from 'zustand'
import { schedulerApi } from '../api/scheduler'
import type {
  ScheduledJob,
  CreateScheduledJobRequest,
  UpdateScheduledJobRequest,
} from '../services/api-types'

interface SchedulerState {
  jobs: ScheduledJob[]
  isLoading: boolean
  error: string | null
  showJobForm: boolean
  editingJobId: string | null  // null = create mode, string = edit mode

  // Actions
  openCreateJob: () => void
  openEditJob: (id: string) => void
  closeJobForm: () => void
  fetchJobs: (entityType?: string) => Promise<void>
  createJob: (req: CreateScheduledJobRequest) => Promise<ScheduledJob>
  updateJob: (id: string, req: UpdateScheduledJobRequest) => Promise<ScheduledJob>
  deleteJob: (id: string) => Promise<void>
  toggleJob: (id: string, enabled: boolean) => Promise<ScheduledJob>
  triggerJob: (id: string) => Promise<{ session_id: string }>
}

export const useSchedulerStore = create<SchedulerState>()((set, get) => ({
  jobs: [],
  isLoading: false,
  error: null,
  showJobForm: false,
  editingJobId: null,

  openCreateJob: () => set({ showJobForm: true, editingJobId: null }),
  openEditJob: (id) => set({ showJobForm: true, editingJobId: id }),
  closeJobForm: () => set({ showJobForm: false, editingJobId: null }),

  fetchJobs: async (entityType?: string) => {
    set({ isLoading: true, error: null })
    try {
      const response = await schedulerApi.listJobs(entityType ? { entity_type: entityType } : undefined)
      set({ jobs: response.jobs, isLoading: false })
    } catch (err) {
      set({ error: String(err), isLoading: false })
    }
  },

  createJob: async (req) => {
    const job = await schedulerApi.createJob(req)
    set((state) => ({ jobs: [job, ...state.jobs] }))
    return job
  },

  updateJob: async (id, req) => {
    const job = await schedulerApi.updateJob(id, req)
    set((state) => ({
      jobs: state.jobs.map((j) => (j.id === id ? job : j)),
    }))
    return job
  },

  deleteJob: async (id) => {
    await schedulerApi.deleteJob(id)
    set((state) => ({ jobs: state.jobs.filter((j) => j.id !== id) }))
  },

  toggleJob: async (id, enabled) => {
    const job = enabled
      ? await schedulerApi.enableJob(id)
      : await schedulerApi.disableJob(id)
    set((state) => ({
      jobs: state.jobs.map((j) => (j.id === id ? job : j)),
    }))
    return job
  },

  triggerJob: async (id) => {
    return schedulerApi.triggerJob(id)
  },
}))
