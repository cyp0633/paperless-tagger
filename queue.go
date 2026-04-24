package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"sync"
	"time"
)

const maxHistoryJobs = 500 // max completed/failed jobs retained in the file

// queueFile is the on-disk representation of the queue
type queueFile struct {
	NextID uint   `json:"next_id"`
	Jobs   []*Job `json:"jobs"`
}

// Queue manages the document processing work queue using in-memory state backed by a JSON file
type Queue struct {
	mu          sync.Mutex
	jobs        []*Job
	nextID      uint
	filePath    string
	sem         chan struct{} // semaphore for concurrency control
	semSize     int
	semMu       sync.Mutex
	subscribers []chan SSEEvent
	subMu       sync.RWMutex
	workerLoop  chan struct{} // signals the worker loop to check for new jobs
}

// NewQueue creates and starts the queue, loading any persisted state from filePath
func NewQueue(filePath string, concurrency int) *Queue {
	if concurrency < 1 {
		concurrency = 1
	}
	q := &Queue{
		filePath:   filePath,
		sem:        make(chan struct{}, concurrency),
		semSize:    concurrency,
		workerLoop: make(chan struct{}, 100),
	}
	q.load()

	// Resume any jobs stuck in "processing" state from before a restart
	q.mu.Lock()
	for _, job := range q.jobs {
		if job.Status == JobStatusProcessing {
			job.Status = JobStatusQueued
			job.StartedAt = nil
			job.UpdatedAt = time.Now()
		}
	}
	q.mu.Unlock()
	q.persist()

	go q.run()
	return q
}

// load reads queue state from disk; errors are logged and ignored (starts empty)
func (q *Queue) load() {
	data, err := os.ReadFile(q.filePath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[Queue] Could not read queue file: %v", err)
		}
		q.nextID = 1
		return
	}
	var qf queueFile
	if err := json.Unmarshal(data, &qf); err != nil {
		log.Printf("[Queue] Could not parse queue file: %v (starting empty)", err)
		q.nextID = 1
		return
	}
	q.nextID = qf.NextID
	if q.nextID < 1 {
		q.nextID = 1
	}
	q.jobs = qf.Jobs
	log.Printf("[Queue] Loaded %d jobs from %s", len(q.jobs), q.filePath)
}

// persist writes queue state to disk; must be called with q.mu held
func (q *Queue) persist() {
	q.trim()
	qf := queueFile{NextID: q.nextID, Jobs: q.jobs}
	data, err := json.MarshalIndent(qf, "", "  ")
	if err != nil {
		log.Printf("[Queue] Marshal error: %v", err)
		return
	}
	if err := os.WriteFile(q.filePath, data, 0644); err != nil {
		log.Printf("[Queue] Write error: %v", err)
	}
}

// trim removes the oldest completed/failed jobs when history exceeds maxHistoryJobs.
// Must be called with q.mu held.
func (q *Queue) trim() {
	var active, history []*Job
	for _, job := range q.jobs {
		switch job.Status {
		case JobStatusQueued, JobStatusProcessing:
			active = append(active, job)
		default:
			history = append(history, job)
		}
	}
	if len(history) > maxHistoryJobs {
		sort.Slice(history, func(i, j int) bool {
			return history[i].CreatedAt.Before(history[j].CreatedAt)
		})
		history = history[len(history)-maxHistoryJobs:]
	}
	q.jobs = append(active, history...)
}

// SetConcurrency adjusts the concurrency limit at runtime
func (q *Queue) SetConcurrency(n int) {
	if n < 1 {
		n = 1
	}
	q.semMu.Lock()
	defer q.semMu.Unlock()
	q.sem = make(chan struct{}, n)
	q.semSize = n
	log.Printf("[Queue] Concurrency set to %d", n)
}

// Enqueue adds a document to the queue. Returns error if already queued/processing.
func (q *Queue) Enqueue(docID int, docTitle string) (*Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for _, job := range q.jobs {
		if job.DocumentID == docID &&
			(job.Status == JobStatusQueued || job.Status == JobStatusProcessing) {
			return nil, fmt.Errorf("document %d is already in queue (status: %s)", docID, job.Status)
		}
	}

	now := time.Now()
	job := &Job{
		ID:            q.nextID,
		CreatedAt:     now,
		UpdatedAt:     now,
		DocumentID:    docID,
		DocumentTitle: docTitle,
		Status:        JobStatusQueued,
	}
	q.nextID++
	q.jobs = append(q.jobs, job)
	q.persist()

	log.Printf("[Queue] Enqueued document %d (%s), job ID %d", docID, docTitle, job.ID)
	q.broadcast(SSEEvent{Type: "job_added", Data: job})
	select {
	case q.workerLoop <- struct{}{}:
	default:
	}
	return job, nil
}

// Retry enqueues a new job for the same document as a failed job.
func (q *Queue) Retry(jobID uint) (*Job, error) {
	q.mu.Lock()
	var found *Job
	for _, j := range q.jobs {
		if j.ID == jobID {
			found = j
			break
		}
	}
	q.mu.Unlock()

	if found == nil {
		return nil, fmt.Errorf("job %d not found", jobID)
	}
	if found.Status != JobStatusFailed {
		return nil, fmt.Errorf("job %d cannot be retried (status: %s)", jobID, found.Status)
	}
	log.Printf("[Queue] Retrying document %d (%s) from failed job %d", found.DocumentID, found.DocumentTitle, found.ID)
	return q.Enqueue(found.DocumentID, found.DocumentTitle)
}

// Cancel removes a queued job (cannot cancel processing jobs)
func (q *Queue) Cancel(jobID uint) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	for i, job := range q.jobs {
		if job.ID == jobID {
			if job.Status != JobStatusQueued {
				return fmt.Errorf("job %d cannot be cancelled (status: %s)", jobID, job.Status)
			}
			cancelled := *job
			q.jobs = append(q.jobs[:i], q.jobs[i+1:]...)
			q.persist()
			log.Printf("[Queue] Cancelled job %d (document %d)", jobID, cancelled.DocumentID)
			q.broadcast(SSEEvent{Type: "job_cancelled", Data: &cancelled})
			return nil
		}
	}
	return fmt.Errorf("job %d not found", jobID)
}

// GetJobs retrieves jobs filtered by status (empty = all), sorted newest first
func (q *Queue) GetJobs(status JobStatus) ([]*Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	result := make([]*Job, 0)
	for _, job := range q.jobs {
		if status == "" || job.Status == status {
			j := *job
			result = append(result, &j)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result, nil
}

// GetStats returns counts by status
func (q *Queue) GetStats() map[JobStatus]int64 {
	q.mu.Lock()
	defer q.mu.Unlock()

	stats := map[JobStatus]int64{
		JobStatusQueued:     0,
		JobStatusProcessing: 0,
		JobStatusCompleted:  0,
		JobStatusFailed:     0,
	}
	for _, job := range q.jobs {
		stats[job.Status]++
	}
	return stats
}

// Subscribe returns a channel that receives SSE events. Call Unsubscribe when done.
func (q *Queue) Subscribe() chan SSEEvent {
	ch := make(chan SSEEvent, 32)
	q.subMu.Lock()
	q.subscribers = append(q.subscribers, ch)
	q.subMu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber channel
func (q *Queue) Unsubscribe(ch chan SSEEvent) {
	q.subMu.Lock()
	defer q.subMu.Unlock()
	for i, sub := range q.subscribers {
		if sub == ch {
			q.subscribers = append(q.subscribers[:i], q.subscribers[i+1:]...)
			close(ch)
			return
		}
	}
}

// broadcast sends an event to all subscribers (non-blocking)
func (q *Queue) broadcast(event SSEEvent) {
	q.subMu.RLock()
	defer q.subMu.RUnlock()
	for _, ch := range q.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}

// run is the main worker loop that dispatches queued jobs
func (q *Queue) run() {
	log.Println("[Queue] Worker loop started")
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-q.workerLoop:
			q.processNext()
		case <-ticker.C:
			q.processNext()
		}
	}
}

// processNext picks the oldest queued job and dispatches it if a worker slot is free
func (q *Queue) processNext() {
	// Try to acquire a worker slot (non-blocking)
	select {
	case q.sem <- struct{}{}:
	default:
		return // all slots busy
	}

	// Find the oldest queued job and atomically mark it as processing
	q.mu.Lock()
	var nextJob *Job
	for _, job := range q.jobs {
		if job.Status == JobStatusQueued {
			if nextJob == nil || job.CreatedAt.Before(nextJob.CreatedAt) {
				nextJob = job
			}
		}
	}
	if nextJob == nil {
		q.mu.Unlock()
		<-q.sem // release unused slot
		return
	}
	now := time.Now()
	nextJob.Status = JobStatusProcessing
	nextJob.StartedAt = &now
	nextJob.UpdatedAt = now
	jobID := nextJob.ID
	jobCopy := *nextJob
	q.persist()
	q.mu.Unlock()

	q.broadcast(SSEEvent{Type: "job_updated", Data: &jobCopy})

	go func() {
		defer func() { <-q.sem }()
		q.processJob(jobID)
	}()
}

// processJob runs the full OCR + extraction + update pipeline for a job
func (q *Queue) processJob(jobID uint) {
	// Grab a working copy of the job
	q.mu.Lock()
	var job Job
	for _, j := range q.jobs {
		if j.ID == jobID {
			job = *j
			break
		}
	}
	q.mu.Unlock()

	log.Printf("[Queue] Processing job %d (document %d: %s)", job.ID, job.DocumentID, job.DocumentTitle)

	err := q.runPipeline(&job)
	completedAt := time.Now()

	// Write result back to in-slice job
	q.mu.Lock()
	for _, j := range q.jobs {
		if j.ID == jobID {
			j.CompletedAt = &completedAt
			j.UpdatedAt = completedAt
			j.DocumentTitle = job.DocumentTitle // may have been updated during pipeline
			j.ResultJSON = job.ResultJSON
			if err != nil {
				j.Status = JobStatusFailed
				j.ErrorMessage = err.Error()
				log.Printf("[Queue] Job %d FAILED: %v", jobID, err)
			} else {
				j.Status = JobStatusCompleted
				j.ErrorMessage = ""
				log.Printf("[Queue] Job %d COMPLETED", jobID)
			}
			job = *j // refresh copy for broadcast
			break
		}
	}
	q.persist()
	q.mu.Unlock()

	q.broadcast(SSEEvent{Type: "job_updated", Data: &job})
}

// runPipeline executes OCR → extraction → update for a single document
func (q *Queue) runPipeline(job *Job) error {
	s, err := GetSettings()
	if err != nil {
		return fmt.Errorf("could not load settings: %w", err)
	}

	paperless := NewPaperlessClient(s.PaperlessBaseURL, s.PaperlessAPIKey)
	ocrLLM := NewLLMClient(s.OCRLLMBaseURL, s.OCRLLMAPIKey)
	extrLLM := NewLLMClient(s.ExtrLLMBaseURL, s.ExtrLLMAPIKey)
	ocrPipeline := NewOCRPipeline(ocrLLM, paperless)
	extractPipeline := NewExtractionPipeline(extrLLM, paperless)

	// Step 1: Get document details
	doc, err := paperless.GetDocument(job.DocumentID)
	if err != nil {
		return fmt.Errorf("failed to fetch document: %w", err)
	}
	job.DocumentTitle = doc.Title

	// Step 2: OCR – render PDF pages and extract text with vision LLM
	ocrResult, err := ocrPipeline.Run(doc, s)
	if err != nil {
		return fmt.Errorf("OCR failed: %w", err)
	}

	// Step 3: Iterative field extraction
	fields, err := extractPipeline.Run(doc, ocrResult, s)
	if err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}

	// Carry the LLM OCR text so UpdateDocument can write it back to Paperless
	fields.OCRContent = ocrResult.Content()

	// Save result JSON into the job struct (written back to slice in processJob)
	resultData, _ := json.Marshal(fields)
	job.ResultJSON = string(resultData)

	// Step 4: Update document in Paperless (metadata + OCR content)
	if err := paperless.UpdateDocument(doc.ID, fields); err != nil {
		return fmt.Errorf("failed to update document in Paperless: %w", err)
	}

	return nil
}
