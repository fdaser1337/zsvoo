package ui

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Colors for terminal output
const (
	Reset   = "\033[0m"
	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
	White   = "\033[37m"
	Bold    = "\033[1m"
)

// PacmanUI provides pacman-style interface
type PacmanUI struct {
	quiet     bool
	noColor   bool
	termWidth int
}

// NewPacmanUI creates new UI instance
func NewPacmanUI(quiet bool) *PacmanUI {
	return &PacmanUI{
		quiet:     quiet,
		termWidth: getTerminalWidth(),
	}
}

// DisableColor disables colored output
func (p *PacmanUI) DisableColor() {
	p.noColor = true
}

// color returns color code or empty string if disabled
func (p *PacmanUI) color(c string) string {
	if p.noColor || p.quiet {
		return ""
	}
	return c
}

// PrintOperation prints operation header like pacman
func (p *PacmanUI) PrintOperation(op, target string) {
	if p.quiet {
		return
	}

	opColor := p.color(Cyan + Bold)
	targetColor := p.color(Reset)

	switch op {
	case "resolving":
		opColor = p.color(Yellow + Bold)
	case "downloading":
		opColor = p.color(Cyan + Bold)
	case "building":
		opColor = p.color(Yellow + Bold)
	case "installing":
		opColor = p.color(Green + Bold)
	case "removing":
		opColor = p.color(Red + Bold)
	}

	fmt.Printf("%s%s%s %s%s%s\n",
		opColor, op, p.color(Reset),
		targetColor, target, p.color(Reset))
}

// PrintProgress prints pacman-style progress line
func (p *PacmanUI) PrintProgress(current, total int, pkgName, action string) {
	if p.quiet {
		return
	}

	percent := float64(current) / float64(total) * 100

	// Build progress bar
	barWidth := 30
	filled := int(float64(barWidth) * percent / 100)
	empty := barWidth - filled

	bar := strings.Repeat("#", filled) + strings.Repeat("-", empty)

	color := p.color(Green)
	if percent < 30 {
		color = p.color(Red)
	} else if percent < 70 {
		color = p.color(Yellow)
	}

	reset := p.color(Reset)

	// Clear line and print
	fmt.Printf("\r\033[K[%s%s%s] %s%d/%d%s (%s%.0f%%s) %s %s",
		color, bar, reset,
		p.color(White+Bold), current, total, reset,
		p.color(Cyan), percent, reset,
		pkgName, action)

	if current == total {
		fmt.Println() // New line on completion
	}
}

// PrintDownloadProgress prints download progress with speed and ETA
func (p *PacmanUI) PrintDownloadProgress(pkgName string, downloaded, total int64, speed float64) {
	if p.quiet {
		return
	}

	percent := float64(downloaded) / float64(total) * 100

	// Format sizes
	downloadedStr := formatSize(downloaded)
	totalStr := formatSize(total)
	speedStr := formatSpeed(speed)

	// Calculate ETA
	eta := ""
	if speed > 0 {
		remaining := float64(total-downloaded) / speed
		eta = formatDuration(time.Duration(remaining) * time.Second)
	}

	barWidth := 25
	filled := int(float64(barWidth) * percent / 100)
	empty := barWidth - filled
	bar := strings.Repeat("#", filled) + strings.Repeat("-", empty)

	color := p.color(Cyan)
	reset := p.color(Reset)

	fmt.Printf("\r\033[K %s %s[%s%s%s] %s%s/%s%s %s%s/s%s ETA: %s",
		pkgName,
		color, bar, reset,
		p.color(White), downloadedStr, totalStr, reset,
		p.color(Yellow), speedStr, reset,
		eta)

	if downloaded >= total {
		fmt.Println()
	}
}

// PrintSuccess prints success message
func (p *PacmanUI) PrintSuccess(msg string) {
	if p.quiet {
		return
	}
	fmt.Printf("%s✓%s %s\n", p.color(Green+Bold), p.color(Reset), msg)
}

// PrintError prints error message
func (p *PacmanUI) PrintError(msg string) {
	fmt.Fprintf(os.Stderr, "%s✗%s %s\n", p.color(Red+Bold), p.color(Reset), msg)
}

// PrintWarning prints warning message
func (p *PacmanUI) PrintWarning(msg string) {
	if p.quiet {
		return
	}
	fmt.Printf("%s⚠%s %s\n", p.color(Yellow+Bold), p.color(Reset), msg)
}

// PrintInfo prints info message
func (p *PacmanUI) PrintInfo(msg string) {
	if p.quiet {
		return
	}
	fmt.Printf("%sℹ%s %s\n", p.color(Blue), p.color(Reset), msg)
}

// PrintPackageList prints package list like pacman -Q
func (p *PacmanUI) PrintPackageList(packages []PackageInfo) {
	if p.quiet {
		return
	}

	maxNameLen := 0
	for _, pkg := range packages {
		if len(pkg.Name) > maxNameLen {
			maxNameLen = len(pkg.Name)
		}
	}

	for _, pkg := range packages {
		fmt.Printf("%s%s%s %s%s%s\n",
			p.color(Green+Bold), padRight(pkg.Name, maxNameLen+2), p.color(Reset),
			p.color(Cyan), pkg.Version, p.color(Reset))
	}
}

// PrintTransactionSummary prints transaction summary
func (p *PacmanUI) PrintTransactionSummary(toInstall, toRemove, toUpgrade []string) {
	if p.quiet {
		return
	}

	fmt.Println()
	fmt.Printf("%s%sTransaction Summary:%s\n", p.color(Bold), p.color(White), p.color(Reset))

	if len(toInstall) > 0 {
		fmt.Printf("%sInstall:%s %d packages\n", p.color(Green), p.color(Reset), len(toInstall))
		for _, pkg := range toInstall {
			fmt.Printf("  %s+%s %s\n", p.color(Green), p.color(Reset), pkg)
		}
	}

	if len(toRemove) > 0 {
		fmt.Printf("%sRemove:%s %d packages\n", p.color(Red), p.color(Reset), len(toRemove))
		for _, pkg := range toRemove {
			fmt.Printf("  %s-%s %s\n", p.color(Red), p.color(Reset), pkg)
		}
	}

	if len(toUpgrade) > 0 {
		fmt.Printf("%sUpgrade:%s %d packages\n", p.color(Yellow), p.color(Reset), len(toUpgrade))
		for _, pkg := range toUpgrade {
			fmt.Printf("  %s*%s %s\n", p.color(Yellow), p.color(Reset), pkg)
		}
	}

	fmt.Println()
}

// MultiProgress tracks multiple parallel operations
type MultiProgress struct {
	ui    *PacmanUI
	items []ProgressItem
	mu    chan struct{} // semaphore for thread-safe updates
}

// ProgressItem represents single progress item
type ProgressItem struct {
	Name      string
	Progress  float64
	Status    string
	Speed     string
	Completed bool
}

// NewMultiProgress creates multi-item progress tracker
func (p *PacmanUI) NewMultiProgress() *MultiProgress {
	return &MultiProgress{
		ui:    p,
		mu:    make(chan struct{}, 1),
		items: []ProgressItem{},
	}
}

// AddItem adds progress item
func (mp *MultiProgress) AddItem(name string) int {
	mp.mu <- struct{}{}
	defer func() { <-mp.mu }()

	idx := len(mp.items)
	mp.items = append(mp.items, ProgressItem{Name: name})
	return idx
}

// Update updates progress item
func (mp *MultiProgress) Update(idx int, progress float64, status, speed string) {
	if idx < 0 || idx >= len(mp.items) {
		return
	}

	mp.mu <- struct{}{}
	defer func() { <-mp.mu }()

	mp.items[idx].Progress = progress
	mp.items[idx].Status = status
	mp.items[idx].Speed = speed
	mp.items[idx].Completed = progress >= 100

	mp.redraw()
}

// redraw refreshes display
func (mp *MultiProgress) redraw() {
	if mp.ui.quiet {
		return
	}

	// Clear previous lines
	for i := 0; i < len(mp.items)+2; i++ {
		fmt.Printf("\033[A\033[K")
	}

	// Print header
	fmt.Printf("%s%sProgress:%s\n", mp.ui.color(Bold), mp.ui.color(White), mp.ui.color(Reset))

	// Print items
	for _, item := range mp.items {
		bar := mp.renderBar(item.Progress, 20)

		statusColor := mp.ui.color(Yellow)
		if item.Completed {
			statusColor = mp.ui.color(Green)
		} else if item.Progress < 20 {
			statusColor = mp.ui.color(Red)
		}

		speedStr := ""
		if item.Speed != "" {
			speedStr = fmt.Sprintf(" %s%s%s", mp.ui.color(Cyan), item.Speed, mp.ui.color(Reset))
		}

		fmt.Printf(" %s %s%s%s%s\n",
			bar,
			padRight(item.Name, 25),
			statusColor, item.Status, mp.ui.color(Reset),
			speedStr)
	}
}

// renderBar renders progress bar
func (mp *MultiProgress) renderBar(percent float64, width int) string {
	filled := int(float64(width) * percent / 100)
	empty := width - filled

	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)

	color := mp.ui.color(Green)
	if percent < 30 {
		color = mp.ui.color(Red)
	} else if percent < 70 {
		color = mp.ui.color(Yellow)
	}

	return fmt.Sprintf("%s[%s]%s %3.0f%%", color, bar, mp.ui.color(Reset), percent)
}

// PackageInfo for list display
type PackageInfo struct {
	Name    string
	Version string
	Desc    string
}

// Helper functions

func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func formatSpeed(bytesPerSec float64) string {
	return formatSize(int64(bytesPerSec))
}

func padRight(s string, length int) string {
	if len(s) >= length {
		return s
	}
	return s + strings.Repeat(" ", length-len(s))
}

func getTerminalWidth() int {
	// Default to 80 if can't determine
	return 80
}
