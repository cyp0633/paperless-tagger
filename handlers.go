package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// Handlers holds all dependencies for HTTP handlers
type Handlers struct {
	queue   *Queue
	scanner *Scanner
}

// NewHandlers creates a new Handlers instance
func NewHandlers(queue *Queue, scanner *Scanner) *Handlers {
	return &Handlers{queue: queue, scanner: scanner}
}

// --- Settings handlers ---

func (h *Handlers) GetSettings(c *gin.Context) {
	s, err := GetSettings()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, s)
}

func (h *Handlers) SaveSettings(c *gin.Context) {
	var s Settings
	if err := c.ShouldBindJSON(&s); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := SaveSettings(&s); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// Update queue concurrency
	h.queue.SetConcurrency(s.QueueConcurrency)
	c.JSON(http.StatusOK, s)
}

func (h *Handlers) GetDefaultPrompt(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"prompt": DefaultSystemPrompt()})
}

func (h *Handlers) CheckPaperless(c *gin.Context) {
	s, err := GetSettings()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	client := NewPaperlessClient(s.PaperlessBaseURL, s.PaperlessAPIKey)
	result := client.CheckAvailability()
	status := http.StatusOK
	if !result.OK {
		status = http.StatusServiceUnavailable
	}
	c.JSON(status, result)
}

func (h *Handlers) CheckOCRLLM(c *gin.Context) {
	s, err := GetSettings()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	client := NewLLMClient(s.OCRLLMBaseURL, s.OCRLLMAPIKey)
	result := client.CheckAvailability()
	status := http.StatusOK
	if !result.OK {
		status = http.StatusServiceUnavailable
	}
	c.JSON(status, result)
}

func (h *Handlers) CheckExtrLLM(c *gin.Context) {
	s, err := GetSettings()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	client := NewLLMClient(s.ExtrLLMBaseURL, s.ExtrLLMAPIKey)
	result := client.CheckAvailability()
	status := http.StatusOK
	if !result.OK {
		status = http.StatusServiceUnavailable
	}
	c.JSON(status, result)
}

// --- Queue handlers ---

func (h *Handlers) GetJobs(c *gin.Context) {
	statusFilter := JobStatus(c.Query("status"))
	jobs, err := h.queue.GetJobs(statusFilter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	stats := h.queue.GetStats()
	c.JSON(http.StatusOK, gin.H{
		"jobs":  jobs,
		"stats": stats,
	})
}

func (h *Handlers) EnqueueDocument(c *gin.Context) {
	var req struct {
		DocumentID    int    `json:"document_id" binding:"required"`
		DocumentTitle string `json:"document_title"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	job, err := h.queue.Enqueue(req.DocumentID, req.DocumentTitle)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, job)
}

func (h *Handlers) RetryJob(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid job ID"})
		return
	}
	job, err := h.queue.Retry(uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, job)
}

func (h *Handlers) CancelJob(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid job ID"})
		return
	}
	if err := h.queue.Cancel(uint(id)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "job cancelled"})
}

// QueueEvents streams Server-Sent Events for queue state changes
func (h *Handlers) QueueEvents(c *gin.Context) {
	ch := h.queue.Subscribe()
	defer h.queue.Unsubscribe(ch)

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	// Send initial stats
	stats := h.queue.GetStats()
	sendSSE(c, SSEEvent{Type: "stats", Data: stats})

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	notify := c.Request.Context().Done()
	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return
			}
			sendSSE(c, event)
			c.Writer.Flush()
		case <-ticker.C:
			fmt.Fprintf(c.Writer, ": heartbeat\n\n")
			c.Writer.Flush()
		case <-notify:
			return
		}
	}
}

func sendSSE(c *gin.Context, event SSEEvent) {
	data, _ := json.Marshal(event.Data)
	fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event.Type, string(data))
}

// --- Scanner handlers ---

func (h *Handlers) TriggerScan(c *gin.Context) {
	h.scanner.TriggerScan()
	c.JSON(http.StatusOK, gin.H{"message": "scan triggered"})
}

// --- Paperless proxy (for fetching documents list in UI) ---

func (h *Handlers) GetPaperlessDocuments(c *gin.Context) {
	s, err := GetSettings()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	client := NewPaperlessClient(s.PaperlessBaseURL, s.PaperlessAPIKey)
	docs, err := client.GetUntaggedDocuments()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"documents": docs, "count": len(docs)})
}
