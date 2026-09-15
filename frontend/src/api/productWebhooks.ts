import axios from 'axios'
import { getApiBaseUrl, getAuthToken } from '../services/api'

export interface ProductAPITrigger {
  id: string
  name: string
  enabled: boolean
  message: string
  auth_mode: 'bearer' | 'github'
  path: string
  secret?: string
}

export type ProductTriggerScope = { profileId: string; projectId: string }

function config() {
  const token = getAuthToken()
  return { baseURL: getApiBaseUrl(), headers: token ? { Authorization: `Bearer ${token}` } : {} }
}

export const productWebhooksApi = {
  list: (scope: ProductTriggerScope) => axios.get<{ triggers: ProductAPITrigger[] }>('/api/product-webhooks', {
    ...config(), params: { profile_id: scope.profileId, project_id: scope.projectId },
  }).then(response => response.data),
  save: (scope: ProductTriggerScope, trigger: ProductAPITrigger, rotateSecret = false) => axios.put<ProductAPITrigger>(`/api/product-webhooks/${encodeURIComponent(trigger.id)}`, {
    profile_id: scope.profileId, project_id: scope.projectId, name: trigger.name,
    message: trigger.message, enabled: trigger.enabled, auth_mode: trigger.auth_mode,
    rotate_secret: rotateSecret,
  }, config()).then(response => response.data),
  delete: (scope: ProductTriggerScope, id: string) => axios.delete(`/api/product-webhooks/${encodeURIComponent(id)}`, {
    ...config(), params: { profile_id: scope.profileId, project_id: scope.projectId },
  }),
}

export function apiTriggerURL(path: string): string {
  return new URL(path, getApiBaseUrl() || window.location.origin).href
}
