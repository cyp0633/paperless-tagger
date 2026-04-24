import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import type { Settings, CheckResult } from '../types'
import {
  getSettings,
  saveSettings,
  checkPaperless,
  checkOCRLLM,
  checkExtrLLM,
  getDefaultPrompt,
} from '../api/client'

interface ConnectionStatus {
  result: CheckResult | null
  loading: boolean
}

const DEFAULT_SETTINGS: Settings = {
  paperless_base_url: '',
  paperless_api_key: '',
  ocr_llm_base_url: '',
  ocr_llm_api_key: '',
  ocr_llm_model: '',
  ocr_max_pages: 20,
  ocr_pages_per_batch: 1,
  ocr_max_tokens: 4096,
  extr_llm_base_url: '',
  extr_llm_api_key: '',
  extr_llm_model: '',
  extr_max_input_chars: 12000,
  extr_max_tokens: 1024,
  extr_max_rounds: 5,
  llm_system_prompt: '',
  scan_interval_minutes: 30,
  queue_concurrency: 2,
}

export default function SettingsPage() {
  const { t } = useTranslation()
  const [form, setForm] = useState<Settings>(DEFAULT_SETTINGS)
  const [saving, setSaving] = useState(false)
  const [saveMsg, setSaveMsg] = useState<{ ok: boolean; text: string } | null>(null)
  const [paperlessCheck, setPaperlessCheck] = useState<ConnectionStatus>({ result: null, loading: false })
  const [ocrCheck, setOcrCheck] = useState<ConnectionStatus>({ result: null, loading: false })
  const [extrCheck, setExtrCheck] = useState<ConnectionStatus>({ result: null, loading: false })
  const [restoringPrompt, setRestoringPrompt] = useState(false)
  const [defaultPrompt, setDefaultPrompt] = useState('')
  const [promptFollowsDefault, setPromptFollowsDefault] = useState(false)

  useEffect(() => {
    Promise.all([getSettings(), getDefaultPrompt()])
      .then(([settings, { prompt }]) => {
        setDefaultPrompt(prompt)
        if (settings.llm_system_prompt === '') {
          setForm({ ...settings, llm_system_prompt: prompt })
          setPromptFollowsDefault(true)
          return
        }
        setForm(settings)
        setPromptFollowsDefault(false)
      })
      .catch(console.error)
  }, [])

  const handleChange = (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => {
    const { name, value, type } = e.target
    if (name === 'llm_system_prompt') {
      setPromptFollowsDefault(false)
    }
    setForm((prev) => ({
      ...prev,
      [name]: type === 'number' ? Number(value) : value,
    }))
  }

  const doSave = async () => {
    const payload: Settings = {
      ...form,
      llm_system_prompt: promptFollowsDefault ? '' : form.llm_system_prompt,
    }
    await saveSettings(payload)
  }

  const handleSave = async () => {
    setSaving(true)
    setSaveMsg(null)
    try {
      await doSave()
      setSaveMsg({ ok: true, text: t('settings.saved') })
    } catch (e: unknown) {
      setSaveMsg({ ok: false, text: t('settings.saveError') + ': ' + (e instanceof Error ? e.message : String(e)) })
    } finally {
      setSaving(false)
      setTimeout(() => setSaveMsg(null), 3000)
    }
  }

  const handleCheckPaperless = async () => {
    setPaperlessCheck({ result: null, loading: true })
    try { await doSave() } catch {}
    setPaperlessCheck({ result: await checkPaperless(), loading: false })
  }

  const handleCheckOCR = async () => {
    setOcrCheck({ result: null, loading: true })
    try { await doSave() } catch {}
    setOcrCheck({ result: await checkOCRLLM(), loading: false })
  }

  const handleCheckExtr = async () => {
    setExtrCheck({ result: null, loading: true })
    try { await doSave() } catch {}
    setExtrCheck({ result: await checkExtrLLM(), loading: false })
  }

  const handleRestorePrompt = async () => {
    setRestoringPrompt(true)
    try {
      let prompt = defaultPrompt
      if (!prompt) {
        const res = await getDefaultPrompt()
        prompt = res.prompt
        setDefaultPrompt(prompt)
      }
      setForm((prev) => ({ ...prev, llm_system_prompt: prompt }))
      setPromptFollowsDefault(true)
    } finally {
      setRestoringPrompt(false)
    }
  }

  return (
    <div className="max-w-3xl mx-auto px-4 py-8 space-y-8">
      <h1 className="text-2xl font-semibold text-gray-900 dark:text-white">{t('settings.title')}</h1>

      {/* Paperless */}
      <Section title={t('settings.paperless.title')}>
        <Field label={t('settings.paperless.baseUrl')} name="paperless_base_url"
          value={form.paperless_base_url} onChange={handleChange}
          placeholder={t('settings.paperless.baseUrlPlaceholder')} />
        <Field label={t('settings.paperless.apiKey')} name="paperless_api_key"
          value={form.paperless_api_key} onChange={handleChange}
          placeholder={t('settings.paperless.apiKeyPlaceholder')} type="password" />
        <CheckRow
          label={t('settings.paperless.check')}
          loadingLabel={t('settings.paperless.checking')}
          status={paperlessCheck}
          onCheck={handleCheckPaperless}
        />
      </Section>

      {/* OCR LLM */}
      <Section title={t('settings.ocrLlm.title')}>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
          <Field label={t('settings.ocrLlm.baseUrl')} name="ocr_llm_base_url"
            value={form.ocr_llm_base_url} onChange={handleChange}
            placeholder={t('settings.ocrLlm.baseUrlPlaceholder')} />
          <Field label={t('settings.ocrLlm.apiKey')} name="ocr_llm_api_key"
            value={form.ocr_llm_api_key} onChange={handleChange}
            placeholder={t('settings.ocrLlm.apiKeyPlaceholder')} type="password" />
          <Field label={t('settings.ocrLlm.model')} name="ocr_llm_model"
            value={form.ocr_llm_model} onChange={handleChange}
            placeholder={t('settings.ocrLlm.modelPlaceholder')} />
          <Field label={t('settings.ocrLlm.maxTokens')} name="ocr_max_tokens"
            value={String(form.ocr_max_tokens)} onChange={handleChange} type="number" min={256} />
          <Field label={t('settings.ocrLlm.maxPages')} name="ocr_max_pages"
            value={String(form.ocr_max_pages)} onChange={handleChange} type="number" min={0} />
          <Field label={t('settings.ocrLlm.pagesPerBatch')} name="ocr_pages_per_batch"
            value={String(form.ocr_pages_per_batch)} onChange={handleChange} type="number" min={1} />
        </div>
        <CheckRow
          label={t('settings.ocrLlm.check')}
          loadingLabel={t('settings.ocrLlm.checking')}
          status={ocrCheck}
          onCheck={handleCheckOCR}
        />
      </Section>

      {/* Extraction LLM */}
      <Section title={t('settings.extrLlm.title')}>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
          <Field label={t('settings.extrLlm.baseUrl')} name="extr_llm_base_url"
            value={form.extr_llm_base_url} onChange={handleChange}
            placeholder={t('settings.extrLlm.baseUrlPlaceholder')} />
          <Field label={t('settings.extrLlm.apiKey')} name="extr_llm_api_key"
            value={form.extr_llm_api_key} onChange={handleChange}
            placeholder={t('settings.extrLlm.apiKeyPlaceholder')} type="password" />
          <Field label={t('settings.extrLlm.model')} name="extr_llm_model"
            value={form.extr_llm_model} onChange={handleChange}
            placeholder={t('settings.extrLlm.modelPlaceholder')} />
          <Field label={t('settings.extrLlm.maxTokens')} name="extr_max_tokens"
            value={String(form.extr_max_tokens)} onChange={handleChange} type="number" min={256} />
          <Field label={t('settings.extrLlm.maxInputChars')} name="extr_max_input_chars"
            value={String(form.extr_max_input_chars)} onChange={handleChange} type="number" min={1000} />
          <Field
            label={t('settings.extrLlm.maxRounds')} name="extr_max_rounds"
            value={String(form.extr_max_rounds)} onChange={handleChange}
            type="number" min={1} max={20}
            help={t('settings.extrLlm.maxRoundsHelp')} />
        </div>
        <CheckRow
          label={t('settings.extrLlm.check')}
          loadingLabel={t('settings.extrLlm.checking')}
          status={extrCheck}
          onCheck={handleCheckExtr}
        />
      </Section>

      {/* Scanner & Queue */}
      <Section title="">
        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
          <div>
            <h3 className="text-base font-medium text-gray-800 dark:text-gray-200 mb-3">
              {t('settings.scanner.title')}
            </h3>
            <Field label={t('settings.scanner.interval')} name="scan_interval_minutes"
              value={String(form.scan_interval_minutes)} onChange={handleChange}
              type="number" min={1} help={t('settings.scanner.intervalHelp')} />
          </div>
          <div>
            <h3 className="text-base font-medium text-gray-800 dark:text-gray-200 mb-3">
              {t('settings.queue.title')}
            </h3>
            <Field label={t('settings.queue.concurrency')} name="queue_concurrency"
              value={String(form.queue_concurrency)} onChange={handleChange}
              type="number" min={1} max={10} help={t('settings.queue.concurrencyHelp')} />
          </div>
        </div>
      </Section>

      {/* System Prompt */}
      <Section title="">
        <div className="flex items-center justify-between mb-3">
          <h3 className="text-base font-medium text-gray-800 dark:text-gray-200">
            {t('settings.prompt.title')}
          </h3>
          <button onClick={handleRestorePrompt} disabled={restoringPrompt} className="btn btn-ghost text-sm">
            {restoringPrompt ? t('settings.prompt.restoring') : t('settings.prompt.restore')}
          </button>
        </div>
        <div className="bg-blue-50 dark:bg-blue-900/20 rounded-lg p-3 space-y-1 mb-3">
          <p className="text-sm font-medium text-blue-700 dark:text-blue-300">{t('settings.prompt.placeholders')}</p>
          <p className="text-xs text-blue-600 dark:text-blue-400 font-mono">{t('settings.prompt.placeholderTags')}</p>
          <p className="text-xs text-blue-600 dark:text-blue-400 font-mono">{t('settings.prompt.placeholderCorrespondents')}</p>
          <p className="text-xs text-blue-600 dark:text-blue-400 font-mono">{t('settings.prompt.placeholderDocumentTypes')}</p>
        </div>
        <div className="bg-yellow-50 dark:bg-yellow-900/20 rounded-lg p-3 mb-3">
          <p className="text-xs text-yellow-700 dark:text-yellow-300">
            {t('settings.prompt.needMoreNote')}
          </p>
        </div>
        <textarea
          name="llm_system_prompt"
          value={form.llm_system_prompt}
          onChange={handleChange}
          rows={14}
          className="w-full font-mono text-sm rounded-lg border border-gray-300 dark:border-gray-600 bg-gray-50 dark:bg-gray-900 text-gray-900 dark:text-gray-100 p-3 focus:outline-none focus:ring-2 focus:ring-blue-500"
        />
      </Section>

      {/* Save */}
      <div className="flex items-center gap-4">
        <button onClick={handleSave} disabled={saving} className="btn btn-primary px-8">
          {saving ? t('settings.saving') : t('settings.save')}
        </button>
        {saveMsg && (
          <span className={`text-sm font-medium ${saveMsg.ok ? 'text-green-600' : 'text-red-600'}`}>
            {saveMsg.text}
          </span>
        )}
      </div>
    </div>
  )
}

// --- Sub-components ---

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="bg-white dark:bg-gray-800 rounded-xl shadow-sm border border-gray-200 dark:border-gray-700 p-6 space-y-4">
      {title && (
        <h2 className="text-lg font-medium text-gray-900 dark:text-white">{title}</h2>
      )}
      {children}
    </section>
  )
}

interface FieldProps {
  label: string
  name: string
  value: string
  onChange: (e: React.ChangeEvent<HTMLInputElement>) => void
  placeholder?: string
  type?: string
  min?: number
  max?: number
  help?: string
}

function Field({ label, name, value, onChange, placeholder, type = 'text', min, max, help }: FieldProps) {
  return (
    <div>
      <label htmlFor={name} className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
        {label}
      </label>
      <input
        id={name} name={name} type={type} value={value}
        onChange={onChange} placeholder={placeholder} min={min} max={max}
        className="w-full rounded-lg border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
      />
      {help && <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">{help}</p>}
    </div>
  )
}

function CheckRow({
  label, loadingLabel, status, onCheck,
}: {
  label: string
  loadingLabel: string
  status: ConnectionStatus
  onCheck: () => void
}) {
  const { t } = useTranslation()
  return (
    <div className="flex items-center gap-3 pt-1">
      <button onClick={onCheck} disabled={status.loading} className="btn btn-secondary">
        {status.loading ? loadingLabel : label}
      </button>
      {status.result && (
        <span className={`text-sm font-medium ${status.result.ok ? 'text-green-600' : 'text-red-600'}`}>
          {status.result.ok
            ? `✓ ${t('settings.check.success')}: ${status.result.message}`
            : `✗ ${t('settings.check.failed')}: ${status.result.message}`}
        </span>
      )}
    </div>
  )
}
