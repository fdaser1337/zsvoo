package builder

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"sync"
	"time"
)

// ThermalMonitor monitors CPU temperature and adjusts parallelism
type ThermalMonitor struct {
	targetTemp   float64 // target CPU temperature (Celsius)
	currentTemp  float64
	maxJobs      int
	currentJobs  int
	cooldownTime time.Duration
	mu           sync.RWMutex
	tempSensor   TempSensor
}

// TempSensor reads CPU temperature
type TempSensor interface {
	Read() (float64, error)
}

// NewThermalMonitor creates thermal monitor
func NewThermalMonitor(targetTemp float64, maxJobs int) *ThermalMonitor {
	return &ThermalMonitor{
		targetTemp:   targetTemp,
		maxJobs:      maxJobs,
		currentJobs:  1, // Start conservative
		cooldownTime: 5 * time.Second,
		tempSensor:   &MacOSTempSensor{},
	}
}

// GetJobs returns current safe job count
func (tm *ThermalMonitor) GetJobs() int {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.currentJobs
}

// Update reads temperature and adjusts parallelism
func (tm *ThermalMonitor) Update() error {
	temp, err := tm.tempSensor.Read()
	if err != nil {
		return err
	}

	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.currentTemp = temp

	// Adjust job count based on temperature
	if temp > tm.targetTemp+10 {
		// Too hot - reduce jobs
		if tm.currentJobs > 1 {
			tm.currentJobs--
			tm.cooldownTime = 10 * time.Second
		}
	} else if temp < tm.targetTemp-5 {
		// Cool enough - increase jobs
		if tm.currentJobs < tm.maxJobs {
			tm.currentJobs++
			tm.cooldownTime = 2 * time.Second
		}
	}

	return nil
}

// ShouldCooldown returns true if we need to cool down
func (tm *ThermalMonitor) ShouldCooldown() bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.currentTemp > tm.targetTemp
}

// Cooldown returns current cooldown duration
func (tm *ThermalMonitor) Cooldown() time.Duration {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.cooldownTime
}

// MacOSTempSensor reads temperature on macOS
type MacOSTempSensor struct{}

func (m *MacOSTempSensor) Read() (float64, error) {
	// Use powermetrics or thermal tools on macOS
	// Fallback to simple load-based estimate

	// Try to read from SMC (requires priviliges)
	cmd := exec.Command("powermetrics", "-n", "1", "--samplers", "smc")
	output, err := cmd.Output()
	if err == nil {
		// Parse temperature from output
		// This is simplified - real implementation would parse SMC output
		_ = output
		return 70.0, nil // Default estimate
	}

	// Estimate based on load
	load := runtime.NumCPU()
	baseTemp := 45.0
	return baseTemp + float64(load)*2.5, nil
}

// ParallelBuilder builds packages with thermal management
type ParallelBuilder struct {
	monitor   *ThermalMonitor
	semaphore chan struct{}
	jobs      map[string]*BuildJob
	mu        sync.RWMutex
}

// BuildJob represents a build job
type BuildJob struct {
	Name         string
	RecipePath   string
	Dependencies []string
	Status       BuildStatus
	Result       error
}

// BuildStatus represents job status
type BuildStatus int

const (
	BuildPending BuildStatus = iota
	BuildRunning
	BuildDone
	BuildFailed
)

// NewParallelBuilder creates builder with thermal management
func NewParallelBuilder(targetTemp float64, maxJobs int) *ParallelBuilder {
	monitor := NewThermalMonitor(targetTemp, maxJobs)

	return &ParallelBuilder{
		monitor:   monitor,
		semaphore: make(chan struct{}, maxJobs),
		jobs:      make(map[string]*BuildJob),
	}
}

// AddJob adds build job
func (pb *ParallelBuilder) AddJob(job *BuildJob) {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	pb.jobs[job.Name] = job
}

// BuildAll builds all jobs respecting dependencies and thermal limits
func (pb *ParallelBuilder) BuildAll(ctx context.Context) error {
	for {
		// Update thermal status
		pb.monitor.Update()

		// Get available job slots
		slots := pb.monitor.GetJobs()

		// Find ready jobs
		ready := pb.getReadyJobs()
		if len(ready) == 0 && pb.allDone() {
			break
		}

		// Launch jobs within thermal limits
		for i := 0; i < min(len(ready), slots); i++ {
			job := ready[i]
			go pb.buildJob(ctx, job)
		}

		// Cool down if needed
		if pb.monitor.ShouldCooldown() {
			time.Sleep(pb.monitor.Cooldown())
		} else {
			time.Sleep(100 * time.Millisecond)
		}
	}

	return nil
}

func (pb *ParallelBuilder) getReadyJobs() []*BuildJob {
	pb.mu.RLock()
	defer pb.mu.RUnlock()

	var ready []*BuildJob
	for _, job := range pb.jobs {
		if job.Status != BuildPending {
			continue
		}

		// Check if dependencies are done
		depsDone := true
		for _, dep := range job.Dependencies {
			if depJob, ok := pb.jobs[dep]; ok {
				if depJob.Status != BuildDone {
					depsDone = false
					break
				}
			}
		}

		if depsDone {
			ready = append(ready, job)
		}
	}

	return ready
}

func (pb *ParallelBuilder) allDone() bool {
	pb.mu.RLock()
	defer pb.mu.RUnlock()

	for _, job := range pb.jobs {
		if job.Status == BuildPending || job.Status == BuildRunning {
			return false
		}
	}
	return true
}

func (pb *ParallelBuilder) buildJob(ctx context.Context, job *BuildJob) {
	pb.mu.Lock()
	job.Status = BuildRunning
	pb.mu.Unlock()

	// Acquire semaphore slot
	pb.semaphore <- struct{}{}
	defer func() { <-pb.semaphore }()

	// Build
	// TODO: actual build

	pb.mu.Lock()
	job.Status = BuildDone
	pb.mu.Unlock()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// AdaptiveParallelBuilder is the main interface for parallel thermal-aware builds
type AdaptiveParallelBuilder struct {
	targetTemp float64
	maxJobs    int
	workDir    string
}

// NewAdaptiveParallelBuilder creates adaptive builder
func NewAdaptiveParallelBuilder(workDir string, targetTemp float64, maxJobs int) *AdaptiveParallelBuilder {
	return &AdaptiveParallelBuilder{
		targetTemp: targetTemp,
		maxJobs:    maxJobs,
		workDir:    workDir,
	}
}

// Build executes parallel thermal-aware build
func (apb *AdaptiveParallelBuilder) Build(packages []string) error {
	monitor := NewThermalMonitor(apb.targetTemp, apb.maxJobs)

	fmt.Printf("🌡️  Thermal target: %.1f°C, Max jobs: %d\n", apb.targetTemp, apb.maxJobs)

	for i, pkg := range packages {
		// Update thermal status
		monitor.Update()

		// Build with current job limit
		jobs := monitor.GetJobs()
		fmt.Printf("📦 [%d/%d] Building %s (jobs=%d, temp=%.1f°C)\n",
			i+1, len(packages), pkg, jobs, monitor.currentTemp)

		// Simulate build (replace with actual)
		time.Sleep(2 * time.Second)

		// Cool down if needed
		if monitor.ShouldCooldown() {
			fmt.Printf("   ⏱️  Cooling down for %v...\n", monitor.Cooldown())
			time.Sleep(monitor.Cooldown())
		}
	}

	return nil
}

// GetRecommendedJobs returns recommended job count for system
func GetRecommendedJobs() int {
	cpus := runtime.NumCPU()

	// Conservative for thermals
	if cpus <= 4 {
		return 1
	} else if cpus <= 8 {
		return 2
	}
	return cpus / 4
}

// GetRecommendedTargetTemp returns recommended temperature limit
func GetRecommendedTargetTemp() float64 {
	switch runtime.GOOS {
	case "darwin":
		return 75.0 // Macs run hot
	case "linux":
		return 80.0 // Linux typically has better cooling
	default:
		return 75.0
	}
}
