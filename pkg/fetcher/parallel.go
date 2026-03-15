package fetcher

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

// ParallelDownloader downloads multiple files concurrently with dependency ordering
type ParallelDownloader struct {
	client      *http.Client
	maxParallel int
	progress    ProgressCallback
}

// ProgressCallback reports download progress
type ProgressCallback func(name string, downloaded, total int64)

// DownloadTask represents a download task with dependencies
type DownloadTask struct {
	Name         string
	URL          string
	DstPath      string
	SHA256       string
	Dependencies []string // names of tasks that must complete first
	Size         int64    // expected size for progress
}

// NewParallelDownloader creates downloader
func NewParallelDownloader(maxParallel int) *ParallelDownloader {
	return &ParallelDownloader{
		client: &http.Client{
			Timeout: 5 * time.Minute,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		maxParallel: maxParallel,
	}
}

// SetProgressCallback sets progress callback
func (pd *ParallelDownloader) SetProgressCallback(cb ProgressCallback) {
	pd.progress = cb
}

// DownloadAll downloads all tasks respecting dependencies
func (pd *ParallelDownloader) DownloadAll(ctx context.Context, tasks []DownloadTask) error {
	// Build dependency graph
	graph := newDownloadGraph(tasks)

	// Execute in waves (topological levels)
	for {
		level := graph.NextLevel()
		if len(level) == 0 {
			break
		}

		// Download this level in parallel
		if err := pd.downloadLevel(ctx, level); err != nil {
			return err
		}

		graph.MarkDone(level)
	}

	return nil
}

// downloadLevel downloads one level concurrently
func (pd *ParallelDownloader) downloadLevel(ctx context.Context, tasks []*DownloadTask) error {
	var wg sync.WaitGroup
	errChan := make(chan error, len(tasks))
	semaphore := make(chan struct{}, pd.maxParallel)

	for _, task := range tasks {
		wg.Add(1)
		go func(t *DownloadTask) {
			defer wg.Done()

			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			if err := pd.downloadOne(ctx, t); err != nil {
				errChan <- fmt.Errorf("%s: %w", t.Name, err)
			}
		}(task)
	}

	wg.Wait()
	close(errChan)

	// Check for errors
	for err := range errChan {
		return err
	}

	return nil
}

// downloadOne downloads single file with resume support
func (pd *ParallelDownloader) downloadOne(ctx context.Context, task *DownloadTask) error {
	// Check if already downloaded
	if _, err := os.Stat(task.DstPath); err == nil {
		// Verify hash
		if task.SHA256 != "" {
			// TODO: verify hash
		}
		return nil
	}

	// Create temp file
	tempPath := task.DstPath + ".download"

	// Open file for writing (append if resuming)
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	// Get current size for resume
	stat, _ := file.Stat()
	currentSize := stat.Size()

	// Create request with resume header
	req, err := http.NewRequestWithContext(ctx, "GET", task.URL, nil)
	if err != nil {
		return err
	}

	if currentSize > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", currentSize))
	}

	// Execute request
	resp, err := pd.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Copy with progress
	var downloaded int64 = currentSize
	reader := &progressReader{
		r:        resp.Body,
		total:    task.Size,
		current:  &downloaded,
		callback: pd.progress,
		name:     task.Name,
	}

	if _, err := io.Copy(file, reader); err != nil {
		return err
	}

	file.Close()

	// Atomic rename
	return os.Rename(tempPath, task.DstPath)
}

// progressReader wraps reader for progress tracking
type progressReader struct {
	r        io.Reader
	total    int64
	current  *int64
	callback ProgressCallback
	name     string
}

func (pr *progressReader) Read(p []byte) (n int, err error) {
	n, err = pr.r.Read(p)
	*pr.current += int64(n)
	if pr.callback != nil {
		pr.callback(pr.name, *pr.current, pr.total)
	}
	return n, err
}

// downloadGraph tracks download dependencies
type downloadGraph struct {
	tasks     map[string]*DownloadTask
	remaining map[string]bool
	depsLeft  map[string]int
}

func newDownloadGraph(tasks []DownloadTask) *downloadGraph {
	g := &downloadGraph{
		tasks:     make(map[string]*DownloadTask),
		remaining: make(map[string]bool),
		depsLeft:  make(map[string]int),
	}

	for i := range tasks {
		t := &tasks[i]
		g.tasks[t.Name] = t
		g.remaining[t.Name] = true
		g.depsLeft[t.Name] = len(t.Dependencies)
	}

	return g
}

func (g *downloadGraph) NextLevel() []*DownloadTask {
	var ready []*DownloadTask

	for name, remaining := range g.remaining {
		if !remaining {
			continue
		}

		if g.depsLeft[name] == 0 {
			ready = append(ready, g.tasks[name])
		}
	}

	return ready
}

func (g *downloadGraph) MarkDone(tasks []*DownloadTask) {
	for _, t := range tasks {
		delete(g.remaining, t.Name)

		// Update dependents
		for name, task := range g.tasks {
			for _, dep := range task.Dependencies {
				if dep == t.Name {
					g.depsLeft[name]--
				}
			}
		}
	}
}
