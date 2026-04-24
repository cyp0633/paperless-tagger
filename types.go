package main

import (
	"time"
)

// JobStatus represents the processing state of a queue job
type JobStatus string

const (
	JobStatusQueued     JobStatus = "queued"
	JobStatusProcessing JobStatus = "processing"
	JobStatusCompleted  JobStatus = "completed"
	JobStatusFailed     JobStatus = "failed"
)

// Job represents a single document processing task in the queue
type Job struct {
	ID            uint       `json:"id"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	DocumentID    int        `json:"document_id"`
	DocumentTitle string     `json:"document_title"`
	Status        JobStatus  `json:"status"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	ErrorMessage  string     `json:"error_message,omitempty"`
	ResultJSON    string     `json:"result_json,omitempty"`
}

// ExtractedFields holds the structured data extracted by the LLM
type ExtractedFields struct {
	Title         string   `json:"title"`
	Correspondent string   `json:"correspondent"`
	Tags          []string `json:"tags"`
	DocumentDate  string   `json:"document_date"` // YYYY-MM-DD format
	DocumentType  string   `json:"document_type"`
	Language      string   `json:"language"`
	OCRContent    string   `json:"ocr_content,omitempty"`
}

// PaperlessDocument represents a document from the Paperless-ngx API
type PaperlessDocument struct {
	ID               int    `json:"id"`
	Title            string `json:"title"`
	Content          string `json:"content"`
	Tags             []int  `json:"tags"`
	Correspondent    *int   `json:"correspondent"`
	DocumentType     *int   `json:"document_type"`
	Created          string `json:"created"`
	CreatedDate      string `json:"created_date"`
	OriginalFileName string `json:"original_file_name"`
}

// PaperlessTag represents a tag in Paperless-ngx
type PaperlessTag struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// PaperlessCorrespondent represents a correspondent in Paperless-ngx
type PaperlessCorrespondent struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// PaperlessDocumentType represents a document type in Paperless-ngx
type PaperlessDocumentType struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// PaperlessListResponse is a generic paginated list response from Paperless-ngx
type PaperlessListResponse[T any] struct {
	Count    int    `json:"count"`
	Next     string `json:"next"`
	Previous string `json:"previous"`
	Results  []T    `json:"results"`
}

// DocumentUpdatePayload is used to PATCH a document in Paperless-ngx
type DocumentUpdatePayload struct {
	Title         string `json:"title,omitempty"`
	Correspondent *int   `json:"correspondent,omitempty"`
	DocumentType  *int   `json:"document_type,omitempty"`
	Tags          []int  `json:"tags,omitempty"`
	CreatedDate   string `json:"created_date,omitempty"`
	Content       string `json:"content,omitempty"`
}

// SSEEvent represents a Server-Sent Event payload
type SSEEvent struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// CheckResult holds the result of a connectivity check
type CheckResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}
