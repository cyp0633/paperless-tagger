export type JobStatus = 'queued' | 'processing' | 'completed' | 'failed'

export interface Job {
  id: number
  document_id: number
  document_title: string
  status: JobStatus
  created_at: string
  started_at?: string
  completed_at?: string
  error_message?: string
  result_json?: string
}

export interface ExtractedFields {
  title: string
  correspondent: string
  tags: string[]
  document_date: string
  document_type: string
  language: string
  ocr_content?: string
}

export interface Settings {
  // Paperless-ngx
  paperless_base_url: string
  paperless_api_key: string

  // OCR LLM
  ocr_llm_base_url: string
  ocr_llm_api_key: string
  ocr_llm_model: string
  ocr_max_pages: number
  ocr_pages_per_batch: number
  ocr_max_tokens: number

  // Extraction LLM
  extr_llm_base_url: string
  extr_llm_api_key: string
  extr_llm_model: string
  extr_max_input_chars: number
  extr_max_tokens: number
  extr_max_rounds: number

  // Prompt & scheduling
  llm_system_prompt: string
  scan_interval_minutes: number
  queue_concurrency: number
}

export interface CheckResult {
  ok: boolean
  message: string
}

export interface QueueStats {
  queued: number
  processing: number
  completed: number
  failed: number
}
