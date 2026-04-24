package main

import (
	"encoding/json"
	"log"
	"os"
	"sync"
)

const defaultSystemPrompt = `You are a personalized document analyzer embedded in a paperless document management system.

Analyze the document content and extract the following information. Output ONLY a valid JSON object with exactly these keys — no explanation or text outside the JSON.

{
  "title": string,
  "correspondent": string | null,
  "tags": string[],
  "document_date": string | null,
  "document_type": string,
  "language": string
}

---

**title**
- The primary goal is DISTINGUISHABILITY: the title must allow this document to be told apart from other documents of the same type.
- For form/table documents (审查表、情况表、申报表 etc.): include the primary subject (person name, book title, project name) followed by the form type. Pattern: "[Subject] [Form Type]"
- For invoices/receipts: prefer the product or service description over the invoice number. Pattern: "[Product/Service] 发票"
- Short and concise. No addresses.
- Use the same language as the document.

**correspondent**
- Identify the PRIMARY issuing or sending party, not the recipient.
- For invoices and receipts: use the seller/vendor, NOT the buyer.
- Use the shortest well-recognized form of the name — drop legal suffixes such as "有限公司"、"股份有限公司"、"Co., Ltd."、"GmbH" unless they are part of the commonly used name. Example: "清华大学出版社" not "清华大学出版社有限公司".
- For well-known brands, always prefer the brand name over the full legal entity name, even if the entity name contains a city or region prefix. Examples: "武汉京东金德贸易有限公司"、"长沙京东厚成贸易有限公司"、"武汉京东德瑞贸易有限公司" → "京东"; "浙江绿源信息科技有限公司" → "绿源"; "上海拼多多网络科技有限公司" → "拼多多"; "深圳顺丰速运有限公司" → "顺丰".
- Prefer an existing correspondent from this list (use exact spelling): {correspondents}
- If none match and a correspondent is clearly identifiable, use the shortened brand/common name as described above.
- If no correspondent can be determined, use null.

**tags**
- Always use Chinese, regardless of the document language, so tags remain reusable across documents.
- FIRST prefer existing tags from this list: {tags}
- Generate 2 to 5 tags. Even for unclear documents, make your best effort — an empty list is almost never correct.
- Think about: topic/domain (e.g., "电商"、"餐饮"、"教育"), financial category (e.g., "消费"、"报销"), relationship (e.g., "工作"、"个人"), or action (e.g., "缴费"、"退款").
- Do NOT include document types (发票、收据、合同、协议 etc.) as tags — those are already captured in document_type.
- Avoid tags that are too generic (e.g., "文件"、"单据") or too specific (e.g., a person's name or order number).
- Focus on thematic categories that help you find related documents across your collection.

**document_date**
- Format: YYYY-MM-DD.
- If multiple dates are present, use the most relevant one (e.g., issue date over expiry date).
- If no date can be determined, use null.

**document_type**
- Must be exactly one of: 发票、收据、合同、协议、证书、表单、报告、通知、说明书、其他
- Choose the closest match.

**language**
- Use BCP 47 language tags: "zh" for Chinese, "en" for English, "de" for German, etc.
- If the language cannot be determined, use "und".

---

Multi-page rule:
If you have seen too little of the document to fill the fields with reasonable confidence, respond ONLY with {"_need_more": true} — you will receive additional pages in the next message. Do NOT use {"_need_more": true} if you already have enough information; output the final JSON directly.

General rule:
If a scalar field cannot be determined from the document content, use null. Always make a best-effort attempt — use null-like values only if the document is completely unintelligible. Do not guess specific facts (dates, names, amounts) that are not present, but thematic tags can be inferred from context.`

// Settings holds all application configuration
type Settings struct {
	// Paperless-ngx connection
	PaperlessBaseURL string `json:"paperless_base_url"`
	PaperlessAPIKey  string `json:"paperless_api_key"`

	// OCR LLM – a vision-capable model used to read document images
	OCRLLMBaseURL    string `json:"ocr_llm_base_url"`
	OCRLLMAPIKey     string `json:"ocr_llm_api_key"`
	OCRLLMModel      string `json:"ocr_llm_model"`
	OCRMaxPages      int    `json:"ocr_max_pages"`
	OCRPagesPerBatch int    `json:"ocr_pages_per_batch"`
	OCRMaxTokens     int    `json:"ocr_max_tokens"`

	// Extraction LLM – used to extract structured fields from OCR text (or images if vision)
	ExtrLLMBaseURL    string `json:"extr_llm_base_url"`
	ExtrLLMAPIKey     string `json:"extr_llm_api_key"`
	ExtrLLMModel      string `json:"extr_llm_model"`
	ExtrLLMVision     bool   `json:"extr_llm_vision"`
	ExtrMaxInputChars int    `json:"extr_max_input_chars"`
	ExtrPagesPerBatch int    `json:"extr_pages_per_batch"`
	ExtrMaxTokens     int    `json:"extr_max_tokens"`
	ExtrMaxRounds     int    `json:"extr_max_rounds"`

	// Prompt & scheduling
	LLMSystemPrompt     string `json:"llm_system_prompt"`
	ScanIntervalMinutes int    `json:"scan_interval_minutes"`
	QueueConcurrency    int    `json:"queue_concurrency"`
}

var (
	settingsMu     sync.RWMutex
	cachedSettings *Settings
	settingsPath   string
)

// InitSettings sets the path for the config JSON file
func InitSettings(path string) {
	settingsPath = path
}

// GetSettings returns settings from cache or loads from disk
func GetSettings() (*Settings, error) {
	settingsMu.RLock()
	if cachedSettings != nil {
		s := *cachedSettings
		settingsMu.RUnlock()
		return &s, nil
	}
	settingsMu.RUnlock()
	return loadSettings()
}

func loadSettings() (*Settings, error) {
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			s := defaultSettings()
			if err := writeSettings(&s); err != nil {
				return nil, err
			}
			settingsMu.Lock()
			cachedSettings = &s
			settingsMu.Unlock()
			return &s, nil
		}
		return nil, err
	}

	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	applySettingsDefaults(&s)

	settingsMu.Lock()
	cachedSettings = &s
	settingsMu.Unlock()
	return &s, nil
}

// applySettingsDefaults fills zero-value fields with sensible defaults
func applySettingsDefaults(s *Settings) {
	if s.OCRMaxPages == 0 {
		s.OCRMaxPages = 20
	}
	if s.OCRPagesPerBatch == 0 {
		s.OCRPagesPerBatch = 1
	}
	if s.OCRMaxTokens == 0 {
		s.OCRMaxTokens = 4096
	}
	if s.ExtrMaxInputChars == 0 {
		s.ExtrMaxInputChars = 12000
	}
	if s.ExtrPagesPerBatch == 0 {
		s.ExtrPagesPerBatch = 5
	}
	if s.ExtrMaxTokens == 0 {
		s.ExtrMaxTokens = 1024
	}
	if s.ExtrMaxRounds == 0 {
		s.ExtrMaxRounds = 5
	}
	if s.ScanIntervalMinutes == 0 {
		s.ScanIntervalMinutes = 30
	}
	if s.QueueConcurrency == 0 {
		s.QueueConcurrency = 2
	}
	// Empty prompt means "follow built-in default prompt".
	// Backward compatibility: older configs persisted the default text directly.
	// Normalize those to empty so future default updates are picked up automatically.
	if s.LLMSystemPrompt == defaultSystemPrompt {
		s.LLMSystemPrompt = ""
	}
}

// SaveSettings persists settings to disk and updates the in-memory cache
func SaveSettings(s *Settings) error {
	if err := writeSettings(s); err != nil {
		return err
	}
	settingsMu.Lock()
	cached := *s
	cachedSettings = &cached
	settingsMu.Unlock()

	select {
	case settingsChanged <- struct{}{}:
	default:
	}
	return nil
}

func writeSettings(s *Settings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		return err
	}
	log.Printf("[Settings] Saved to %s", settingsPath)
	return nil
}

// InvalidateSettingsCache forces the next GetSettings call to re-read from disk
func InvalidateSettingsCache() {
	settingsMu.Lock()
	cachedSettings = nil
	settingsMu.Unlock()
}

// DefaultSystemPrompt returns the built-in system prompt template
func DefaultSystemPrompt() string {
	return defaultSystemPrompt
}

// EffectiveSystemPrompt returns the runtime prompt used by extraction.
// Empty configured prompt means "use built-in default prompt".
func EffectiveSystemPrompt(s *Settings) string {
	if s == nil || s.LLMSystemPrompt == "" {
		return defaultSystemPrompt
	}
	return s.LLMSystemPrompt
}

// defaultSettings populates from environment variables with sensible fallbacks
func defaultSettings() Settings {
	model := os.Getenv("OPENAI_MODEL")
	if model == "" {
		model = "gpt-4o-mini"
	}
	return Settings{
		PaperlessBaseURL: os.Getenv("PAPERLESS_BASE_URL"),
		PaperlessAPIKey:  os.Getenv("PAPERLESS_API_KEY"),

		OCRLLMBaseURL:    os.Getenv("OPENAI_BASE_URL"),
		OCRLLMAPIKey:     os.Getenv("OPENAI_API_KEY"),
		OCRLLMModel:      model,
		OCRMaxPages:      20,
		OCRPagesPerBatch: 1,
		OCRMaxTokens:     4096,

		ExtrLLMBaseURL:    os.Getenv("OPENAI_BASE_URL"),
		ExtrLLMAPIKey:     os.Getenv("OPENAI_API_KEY"),
		ExtrLLMModel:      model,
		ExtrLLMVision:     false,
		ExtrMaxInputChars: 12000,
		ExtrPagesPerBatch: 5,
		ExtrMaxTokens:     1024,
		ExtrMaxRounds:     5,

		LLMSystemPrompt:     "",
		ScanIntervalMinutes: 30,
		QueueConcurrency:    2,
	}
}
