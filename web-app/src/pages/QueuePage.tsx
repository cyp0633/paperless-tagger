import { useState, useEffect, useRef, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import type { Job, JobStatus } from '../types'
import { getJobs, enqueueDocument, cancelJob, retryJob, triggerScan } from '../api/client'

const STATUS_COLORS: Record<JobStatus, string> = {
  queued: 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900/30 dark:text-yellow-300',
  processing: 'bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-300',
  completed: 'bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-300',
  failed: 'bg-red-100 text-red-800 dark:bg-red-900/30 dark:text-red-300',
}

export default function QueuePage() {
  const { t } = useTranslation()
  const [jobs, setJobs] = useState<Job[]>([])
  const [stats, setStats] = useState<Record<JobStatus, number>>({
    queued: 0,
    processing: 0,
    completed: 0,
    failed: 0,
  })
  const [filter, setFilter] = useState<JobStatus | ''>('')
  const [loading, setLoading] = useState(true)
  const [scanning, setScanning] = useState(false)
  const [scanMsg, setScanMsg] = useState('')
  const [newDocId, setNewDocId] = useState('')
  const [adding, setAdding] = useState(false)
  const [retryingJob, setRetryingJob] = useState<number | null>(null)
  const [expandedJob, setExpandedJob] = useState<number | null>(null)
  const sseRef = useRef<EventSource | null>(null)

  const fetchJobs = useCallback(async () => {
    try {
      const data = await getJobs(filter || undefined)
      setJobs(Array.isArray(data.jobs) ? data.jobs : [])
      setStats({
        queued: data.stats?.queued ?? 0,
        processing: data.stats?.processing ?? 0,
        completed: data.stats?.completed ?? 0,
        failed: data.stats?.failed ?? 0,
      })
    } catch (e) {
      console.error('Failed to fetch jobs:', e)
    } finally {
      setLoading(false)
    }
  }, [filter])

  useEffect(() => {
    fetchJobs()
  }, [fetchJobs])

  // Subscribe to SSE for real-time updates
  useEffect(() => {
    const es = new EventSource('/api/queue/events')
    sseRef.current = es

    const handleEvent = () => {
      fetchJobs()
    }
    es.addEventListener('job_added', handleEvent)
    es.addEventListener('job_updated', handleEvent)
    es.addEventListener('job_cancelled', handleEvent)
    es.addEventListener('stats', handleEvent)

    es.onerror = () => {
      // Will auto-reconnect
    }

    return () => {
      es.close()
      sseRef.current = null
    }
  }, [fetchJobs])

  const handleScan = async () => {
    setScanning(true)
    setScanMsg('')
    try {
      await triggerScan()
      setScanMsg(t('queue.scanTriggered'))
      setTimeout(() => setScanMsg(''), 3000)
    } finally {
      setScanning(false)
    }
  }

  const handleAddDocument = async () => {
    const id = parseInt(newDocId)
    if (!id) return
    setAdding(true)
    try {
      await enqueueDocument(id)
      setNewDocId('')
      fetchJobs()
    } catch (e: unknown) {
      alert(e instanceof Error ? e.message : String(e))
    } finally {
      setAdding(false)
    }
  }

  const handleCancel = async (jobId: number) => {
    try {
      await cancelJob(jobId)
      fetchJobs()
    } catch (e: unknown) {
      alert(e instanceof Error ? e.message : String(e))
    }
  }

  const handleRetry = async (jobId: number) => {
    setRetryingJob(jobId)
    try {
      await retryJob(jobId)
      fetchJobs()
    } catch (e: unknown) {
      alert(e instanceof Error ? e.message : String(e))
    } finally {
      setRetryingJob(null)
    }
  }

  const fmtDate = (iso?: string) => {
    if (!iso) return '—'
    return new Date(iso).toLocaleString()
  }

  return (
    <div className="max-w-5xl mx-auto px-4 py-8 space-y-6">
      <div className="flex items-center justify-between flex-wrap gap-4">
        <h1 className="text-2xl font-semibold text-gray-900 dark:text-white">
          {t('queue.title')}
        </h1>
        <div className="flex items-center gap-3">
          {scanMsg && <span className="text-sm text-green-600">{scanMsg}</span>}
          <button
            onClick={handleScan}
            disabled={scanning}
            className="btn btn-primary"
          >
            {scanning ? t('queue.scanning') : t('queue.triggerScan')}
          </button>
        </div>
      </div>

      {/* Stats cards */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        {(['queued', 'processing', 'completed', 'failed'] as JobStatus[]).map((s) => (
          <button
            key={s}
            onClick={() => setFilter(filter === s ? '' : s)}
            className={`rounded-xl p-4 text-center transition-all border-2 ${
              filter === s
                ? 'border-blue-500 ' + STATUS_COLORS[s]
                : 'border-transparent bg-white dark:bg-gray-800 hover:border-gray-300 dark:hover:border-gray-600'
            }`}
          >
            <div className="text-2xl font-bold text-gray-900 dark:text-white">
              {stats[s] ?? 0}
            </div>
            <div className="text-sm text-gray-500 dark:text-gray-400">
              {t(`queue.stats.${s}`)}
            </div>
          </button>
        ))}
      </div>

      {/* Add document */}
      <div className="bg-white dark:bg-gray-800 rounded-xl border border-gray-200 dark:border-gray-700 p-4 flex items-end gap-3">
        <div className="flex-1">
          <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
            {t('queue.documentId')}
          </label>
          <input
            type="number"
            value={newDocId}
            onChange={(e) => setNewDocId(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && handleAddDocument()}
            placeholder="123"
            className="w-full rounded-lg border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
        </div>
        <button
          onClick={handleAddDocument}
          disabled={adding || !newDocId}
          className="btn btn-secondary"
        >
          {adding ? t('queue.adding') : t('queue.add')}
        </button>
      </div>

      {/* Job list */}
      <div className="space-y-3">
        {loading ? (
          <div className="text-center py-12 text-gray-400">{t('common.loading')}</div>
        ) : jobs.length === 0 ? (
          <div className="text-center py-12 text-gray-400 bg-white dark:bg-gray-800 rounded-xl border border-gray-200 dark:border-gray-700">
            {t('queue.noJobs')}
          </div>
        ) : (
          jobs.map((job) => (
            <div
              key={job.id}
              className="bg-white dark:bg-gray-800 rounded-xl border border-gray-200 dark:border-gray-700 overflow-hidden"
            >
              <div className="p-4 flex items-center gap-4">
                {/* Status badge */}
                <span
                  className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${STATUS_COLORS[job.status]}`}
                >
                  {job.status === 'processing' && (
                    <span className="w-2 h-2 rounded-full bg-blue-500 animate-pulse mr-1.5" />
                  )}
                  {t(`queue.status.${job.status}`)}
                </span>

                {/* Document info */}
                <div className="flex-1 min-w-0">
                  <p className="font-medium text-gray-900 dark:text-white truncate">
                    {job.document_title || `Document #${job.document_id}`}
                  </p>
                  <p className="text-xs text-gray-500 dark:text-gray-400">
                    {t('queue.id')} {job.document_id} · {t('queue.createdAt')}: {fmtDate(job.created_at)}
                    {job.started_at && ` · ${t('queue.startedAt')}: ${fmtDate(job.started_at)}`}
                    {job.completed_at && ` · ${t('queue.completedAt')}: ${fmtDate(job.completed_at)}`}
                  </p>
                </div>

                {/* Actions */}
                <div className="flex items-center gap-2 shrink-0">
                  {job.result_json && (
                    <button
                      onClick={() => setExpandedJob(expandedJob === job.id ? null : job.id)}
                      className="text-xs text-blue-600 hover:underline"
                    >
                      {t('queue.viewResult')}
                    </button>
                  )}
                  {job.status === 'queued' && (
                    <button
                      onClick={() => handleCancel(job.id)}
                      className="text-xs text-red-600 hover:underline"
                    >
                      {t('queue.cancel')}
                    </button>
                  )}
                  {job.status === 'failed' && (
                    <button
                      onClick={() => handleRetry(job.id)}
                      disabled={retryingJob === job.id}
                      className="text-xs text-orange-600 hover:underline disabled:opacity-50"
                    >
                      {retryingJob === job.id ? t('queue.retrying') : t('queue.retry')}
                    </button>
                  )}
                </div>
              </div>

              {/* Error message */}
              {job.error_message && (
                <div className="px-4 pb-3">
                  <p className="text-xs text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-900/20 rounded p-2">
                    {job.error_message}
                  </p>
                </div>
              )}

              {/* Expanded result */}
              {expandedJob === job.id && job.result_json && (
                <div className="border-t border-gray-100 dark:border-gray-700 px-4 py-3">
                  <p className="text-xs font-medium text-gray-500 dark:text-gray-400 mb-2">
                    {t('queue.result')}
                  </p>
                  <ResultView json={job.result_json} />
                </div>
              )}
            </div>
          ))
        )}
      </div>
    </div>
  )
}

function ResultView({ json }: { json: string }) {
  try {
    const data = JSON.parse(json)
    return (
      <div className="space-y-1.5 text-sm">
        {Object.entries(data)
          .filter(([k]) => k !== 'ocr_content')
          .map(([key, val]) => (
            <div key={key} className="flex gap-2">
              <span className="font-mono text-xs text-gray-500 dark:text-gray-400 w-32 shrink-0">
                {key}
              </span>
              <span className="text-gray-900 dark:text-gray-100">
                {Array.isArray(val) ? val.join(', ') : String(val)}
              </span>
            </div>
          ))}
      </div>
    )
  } catch {
    return <pre className="text-xs text-gray-600 dark:text-gray-400 overflow-auto">{json}</pre>
  }
}
