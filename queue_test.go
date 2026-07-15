package main

import (
	"sync"
	"testing"
	"time"
)

func newTestQueue(t *testing.T, jobs ...*Job) *Queue {
	t.Helper()
	return &Queue{
		jobs:        jobs,
		nextID:      uint(len(jobs) + 1),
		filePath:    t.TempDir() + "/queue.json",
		workerLoop:  make(chan struct{}, 100),
		subscribers: nil,
	}
}

func failedJob(id uint, documentID int) *Job {
	now := time.Now()
	return &Job{
		ID:            id,
		CreatedAt:     now,
		UpdatedAt:     now,
		DocumentID:    documentID,
		DocumentTitle: "Test document",
		Status:        JobStatusFailed,
	}
}

func TestRetryReusesActiveJobForDocument(t *testing.T) {
	failed := failedJob(1, 42)
	active := &Job{
		ID:            2,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		DocumentID:    failed.DocumentID,
		DocumentTitle: failed.DocumentTitle,
		Status:        JobStatusQueued,
	}
	q := newTestQueue(t, failed, active)

	job, err := q.Retry(failed.ID)
	if err != nil {
		t.Fatalf("Retry() error = %v", err)
	}
	if job.ID != active.ID {
		t.Fatalf("Retry() job ID = %d, want existing active job %d", job.ID, active.ID)
	}
	if len(q.jobs) != 2 {
		t.Fatalf("Retry() created a duplicate job; job count = %d, want 2", len(q.jobs))
	}
}

func TestRetryConcurrentCallsCreateOneJob(t *testing.T) {
	failed := failedJob(1, 42)
	q := newTestQueue(t, failed)

	const retries = 10
	results := make(chan *Job, retries)
	errors := make(chan error, retries)
	var wg sync.WaitGroup
	for range retries {
		wg.Add(1)
		go func() {
			defer wg.Done()
			job, err := q.Retry(failed.ID)
			results <- job
			errors <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errors)

	for err := range errors {
		if err != nil {
			t.Fatalf("Retry() error = %v", err)
		}
	}
	var jobID uint
	for job := range results {
		if jobID == 0 {
			jobID = job.ID
		}
		if job.ID != jobID {
			t.Fatalf("concurrent Retry() returned job %d, want %d", job.ID, jobID)
		}
	}
	if len(q.jobs) != 2 {
		t.Fatalf("concurrent Retry() job count = %d, want 2", len(q.jobs))
	}
}
