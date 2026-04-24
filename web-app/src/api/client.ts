import type { Settings, Job, JobStatus, CheckResult } from '../types'

const BASE = '/api'

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  })
  const data = await res.json()
  if (!res.ok) {
    throw new Error(data.error || `HTTP ${res.status}`)
  }
  return data as T
}

// Settings
export const getSettings = () => request<Settings>('/settings')
export const saveSettings = (s: Settings) =>
  request<Settings>('/settings', { method: 'PUT', body: JSON.stringify(s) })
export const getDefaultPrompt = () =>
  request<{ prompt: string }>('/settings/default-prompt')
export const checkPaperless = () =>
  fetch(`${BASE}/settings/check/paperless`, { method: 'POST' }).then(
    async (r) => (await r.json()) as CheckResult
  )
export const checkOCRLLM = () =>
  fetch(`${BASE}/settings/check/ocr-llm`, { method: 'POST' }).then(
    async (r) => (await r.json()) as CheckResult
  )
export const checkExtrLLM = () =>
  fetch(`${BASE}/settings/check/extr-llm`, { method: 'POST' }).then(
    async (r) => (await r.json()) as CheckResult
  )

// Queue
export interface QueueResponse {
  jobs: Job[]
  stats: Record<JobStatus, number>
}
export const getJobs = (status?: JobStatus) =>
  request<QueueResponse>(`/queue${status ? `?status=${status}` : ''}`)
export const enqueueDocument = (document_id: number, document_title?: string) =>
  request<Job>('/queue', {
    method: 'POST',
    body: JSON.stringify({ document_id, document_title: document_title || '' }),
  })
export const cancelJob = (id: number) =>
  request<{ message: string }>(`/queue/${id}`, { method: 'DELETE' })
export const retryJob = (id: number) =>
  request<Job>(`/queue/${id}/retry`, { method: 'POST' })

// Scanner
export const triggerScan = () =>
  request<{ message: string }>('/scan/trigger', { method: 'POST' })

// Untagged documents
export const getUntaggedDocuments = () =>
  request<{ documents: Array<{ id: number; title: string }>; count: number }>(
    '/documents/untagged'
  )
