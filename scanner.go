package main

import (
	"log"
	"time"
)

// Scanner periodically checks for untagged documents and enqueues them
type Scanner struct {
	queue *Queue
}

// settingsChanged is a channel used to signal the scanner to reload its interval
var settingsChanged = make(chan struct{}, 1)

// NewScanner creates a new scanner
func NewScanner(queue *Queue) *Scanner {
	return &Scanner{queue: queue}
}

// Start launches the background scanning loop
func (s *Scanner) Start() {
	go s.loop()
	log.Println("[Scanner] Background scanner started")
}

// TriggerScan runs a scan immediately (non-blocking)
func (s *Scanner) TriggerScan() {
	select {
	case settingsChanged <- struct{}{}:
	default:
	}
	log.Println("[Scanner] Manual scan triggered")
}

func (s *Scanner) loop() {
	interval := s.loadInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run once at startup
	s.scan()

	for {
		select {
		case <-ticker.C:
			s.scan()
		case <-settingsChanged:
			// Reload interval and reset ticker
			newInterval := s.loadInterval()
			if newInterval != interval {
				interval = newInterval
				ticker.Reset(interval)
				log.Printf("[Scanner] Interval updated to %v", interval)
			}
			// Also run a scan immediately on any settings change
			s.scan()
		}
	}
}

func (s *Scanner) loadInterval() time.Duration {
	settings, err := GetSettings()
	if err != nil || settings.ScanIntervalMinutes < 1 {
		return 30 * time.Minute
	}
	return time.Duration(settings.ScanIntervalMinutes) * time.Minute
}

func (s *Scanner) scan() {
	settings, err := GetSettings()
	if err != nil {
		log.Printf("[Scanner] Could not load settings: %v", err)
		return
	}

	if settings.PaperlessBaseURL == "" || settings.PaperlessAPIKey == "" {
		log.Println("[Scanner] Paperless credentials not configured, skipping scan")
		return
	}

	log.Println("[Scanner] Scanning for untagged documents...")
	client := NewPaperlessClient(settings.PaperlessBaseURL, settings.PaperlessAPIKey)
	docs, err := client.GetUntaggedDocuments()
	if err != nil {
		log.Printf("[Scanner] Error fetching untagged documents: %v", err)
		return
	}

	added := 0
	for _, doc := range docs {
		_, err := s.queue.Enqueue(doc.ID, doc.Title)
		if err != nil {
			log.Printf("[Scanner] Skip document %d (%s): %v", doc.ID, doc.Title, err)
			continue
		}
		added++
	}
	log.Printf("[Scanner] Scan complete: %d new documents enqueued (of %d found)", added, len(docs))
}
