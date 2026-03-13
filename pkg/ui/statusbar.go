package ui

import (
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"zsvo/pkg/i18n"
)

// StatusBar represents an animated status bar
type StatusBar struct {
	pkgName      string
	total        int
	current      int
	startedAt    time.Time
	lastUpdate   time.Time
	lastStep     int
	enabled      bool
	frameIdx     int
	lastLineLen  int
	theme        Theme
}

// Theme defines color scheme for status bar
type Theme struct {
	Spinner      string
	ProgressFull string
	ProgressEmpty string
	Text         string
	Accent       string
	Success      string
	Error        string
	Warning      string
	Info         string
}

// Predefined themes
var Themes = map[string]Theme{
	"neon": {
		Spinner:      "36;1",      // Bright cyan
		ProgressFull: "92",        // Bright green
		ProgressEmpty: "90",       // Dark gray
		Text:         "37",        // White
		Accent:       "93;1",      // Bright magenta
		Success:      "32;1",      // Bright green
		Error:        "31;1",      // Bright red
		Warning:      "33;1",      // Bright yellow
		Info:         "34;1",      // Bright blue
	},
	"matrix": {
		Spinner:      "32;1",      // Bright green
		ProgressFull: "32",        // Green
		ProgressEmpty: "2",        // Dark green
		Text:         "37",        // White
		Accent:       "92;1",      // Bright green
		Success:      "32;1",      // Bright green
		Error:        "31;1",      // Bright red
		Warning:      "33",        // Yellow
		Info:         "36",        // Cyan
	},
	"fire": {
		Spinner:      "33;1",      // Bright yellow
		ProgressFull: "91",        // Bright red
		ProgressEmpty: "90",       // Dark gray
		Text:         "37",        // White
		Accent:       "93;1",      // Bright magenta
		Success:      "32;1",      // Bright green
		Error:        "31;1",      // Bright red
		Warning:      "33;1",      // Bright yellow
		Info:         "34;1",      // Bright blue
	},
	"ocean": {
		Spinner:      "36;1",      // Bright cyan
		ProgressFull: "94",        // Bright blue
		ProgressEmpty: "90",       // Dark gray
		Text:         "37",        // White
		Accent:       "96;1",      // Bright cyan
		Success:      "32;1",      // Bright green
		Error:        "31;1",      // Bright red
		Warning:      "33;1",      // Bright yellow
		Info:         "34;1",      // Bright blue
	},
}

// Advanced spinner animations
var spinners = map[string][]string{
	"classic": {"|", "/", "-", "\\"},
	"dots":    {"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
	"arrows":  {"←", "↖", "↑", "↗", "→", "↘", "↓", "↙"},
	"blocks":  {"▖", "▘", "▝", "▗"},
	"pulse":   {"⚡", "✨", "🔥", "💫", "⭐"},
	"hearts":  {"❤️", "💙", "💚", "💛", "💜", "🧡"},
	"matrix":  {"⚈", "⚉", "⚊", "⚋", "⚌", "⚍", "⚎", "⚏"},
}

func NewStatusBar(pkgName string, total int) *StatusBar {
	return &StatusBar{
		pkgName:    pkgName,
		total:      total,
		startedAt:  time.Now(),
		lastUpdate: time.Now(),
		enabled:    supportsANSIAndTTY(),
		theme:      Themes["neon"], // Default theme
	}
}

// SetTheme changes the status bar theme
func (s *StatusBar) SetTheme(themeName string) {
	if theme, exists := Themes[themeName]; exists {
		s.theme = theme
	}
}

// SetSpinner changes the spinner animation
func (s *StatusBar) SetSpinner(spinnerName string) {
	if _, exists := spinners[spinnerName]; exists {
		s.frameIdx = 0 // Reset animation
	}
}

func (s *StatusBar) Update(current int, message string) {
	if current < 0 {
		current = 0
	}
	if current > s.total {
		current = s.total
	}
	s.current = current

	now := time.Now()
	
	if !s.enabled {
		fmt.Printf("[%d/%d] %s\n", current, s.total, message)
		return
	}

	// Calculate progress
	percent := float64(current) / float64(s.total)
	
	// Get current spinner frame
	spinnerFrames := spinners["dots"]
	spinner := spinnerFrames[s.frameIdx%len(spinnerFrames)]
	s.frameIdx++

	// Build progress bar
	bar := s.renderProgressBar(percent)
	
	// Calculate metrics
	elapsed := time.Since(s.startedAt)
	var speed, eta string
	if current > s.lastStep && !s.lastUpdate.IsZero() {
		timeDiff := now.Sub(s.lastUpdate).Seconds()
		if timeDiff > 0 {
			stepsPerSec := float64(current-s.lastStep) / timeDiff
			if stepsPerSec > 0 {
				remaining := s.total - current
				etaSeconds := float64(remaining) / stepsPerSec
				speed = fmt.Sprintf("%.1f %s", stepsPerSec, i18n.T("steps_per_sec"))
				eta = fmt.Sprintf("%s %s", i18n.T("eta"), formatDuration(time.Duration(etaSeconds)*time.Second))
			}
		}
	}
	
	s.lastUpdate = now
	s.lastStep = current

	// Build status line
	line := s.buildStatusLine(spinner, bar, percent, message, speed, eta, elapsed)
	
	// Clear previous line and print new one
	if pad := s.lastLineLen - visibleLen(line); pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	s.lastLineLen = visibleLen(line)
	
	fmt.Print("\r" + line)
}

func (s *StatusBar) Finish(success bool, message string) {
	if !s.enabled {
		status := i18n.T("failed")
		if success {
			status = i18n.T("success")
		}
		fmt.Printf("%s: %s\n", s.pkgName, status)
		return
	}

	// Show final status
	statusColor := s.theme.Error
	statusText := i18n.T("failed")
	if success {
		statusColor = s.theme.Success
		statusText = i18n.T("completed")
	}

	elapsed := time.Since(s.startedAt)
	
	fmt.Printf("\n%s %s %s (%s)\n",
		colorize(statusColor, "✓"),
		colorize(s.theme.Accent, s.pkgName),
		colorize(statusColor, statusText),
		colorize(s.theme.Text, formatDuration(elapsed)),
	)
}

func (s *StatusBar) renderProgressBar(percent float64) string {
	const width = 25
	filled := int(math.Floor(percent * float64(width)))
	
	if filled > width {
		filled = width
	}

	// Create gradient effect
	fullChars := []string{"█", "▓", "▒", "░"}
	var bar strings.Builder
	
	for i := 0; i < width; i++ {
		if i < filled {
			// Gradient effect
			charIdx := int(float64(i) / float64(width) * float64(len(fullChars)))
			if charIdx >= len(fullChars) {
				charIdx = len(fullChars) - 1
			}
			bar.WriteString(colorize(s.theme.ProgressFull, fullChars[charIdx]))
		} else {
			bar.WriteString(colorize(s.theme.ProgressEmpty, "░"))
		}
	}
	
	return fmt.Sprintf("[%s]", bar.String())
}

func (s *StatusBar) buildStatusLine(spinner, bar string, percent float64, message, speed, eta string, elapsed time.Duration) string {
	percentStr := fmt.Sprintf("%.0f%%", percent*100)
	
	// Truncate message if too long
	maxMsgLen := 30
	if len(message) > maxMsgLen {
		message = message[:maxMsgLen-3] + "..."
	}

	var parts []string
	
	// Main info
	parts = append(parts, colorize(s.theme.Spinner, spinner))
	parts = append(parts, colorize(s.theme.Accent, s.pkgName))
	parts = append(parts, bar)
	parts = append(parts, colorize(s.theme.Text, percentStr))
	parts = append(parts, colorize(s.theme.Info, "| "+message))
	
	// Speed and ETA if available
	if speed != "" && eta != "" {
		parts = append(parts, colorize(s.theme.Warning, speed))
		parts = append(parts, colorize(s.theme.Info, eta))
	}
	
	// Always show elapsed time
	parts = append(parts, colorize(s.theme.Text, formatDuration(elapsed)))
	
	return strings.Join(parts, " ")
}

func (s *StatusBar) PrintHeader(title string) {
	if !s.enabled {
		fmt.Printf("=== %s ===\n", title)
		return
	}
	
	border := colorize(s.theme.Accent, "╔")
	titleColored := colorize(s.theme.Text, fmt.Sprintf(" %s ", title))
	borderEnd := colorize(s.theme.Accent, "╗")
	
	fmt.Printf("%s%s%s\n", border, titleColored, borderEnd)
}

func (s *StatusBar) PrintFooter() {
	if !s.enabled {
		fmt.Printf("===================\n")
		return
	}
	
	border := colorize(s.theme.Accent, "╚")
	borderEnd := colorize(s.theme.Accent, "╝")
	content := colorize(s.theme.Text, strings.Repeat("═", 50))
	
	fmt.Printf("%s%s%s\n", border, content, borderEnd)
}

func (s *StatusBar) PrintInfo(message string) {
	if !s.enabled {
		fmt.Printf("ℹ️  %s\n", message)
		return
	}
	
	icon := colorize(s.theme.Info, "ℹ️")
	text := colorize(s.theme.Text, message)
	fmt.Printf("  %s %s\n", icon, text)
}

func (s *StatusBar) PrintSuccess(message string) {
	if !s.enabled {
		fmt.Printf("✅ %s\n", message)
		return
	}
	
	icon := colorize(s.theme.Success, "✅")
	text := colorize(s.theme.Text, message)
	fmt.Printf("  %s %s\n", icon, text)
}

func (s *StatusBar) PrintWarning(message string) {
	if !s.enabled {
		fmt.Printf("⚠️  %s\n", message)
		return
	}
	
	icon := colorize(s.theme.Warning, "⚠️")
	text := colorize(s.theme.Text, message)
	fmt.Printf("  %s %s\n", icon, text)
}

func (s *StatusBar) PrintError(message string) {
	if !s.enabled {
		fmt.Printf("❌ %s\n", message)
		return
	}
	
	icon := colorize(s.theme.Error, "❌")
	text := colorize(s.theme.Text, message)
	fmt.Printf("  %s %s\n", icon, text)
}

// Helper functions

func colorize(color, text string) string {
	if !supportsANSIAndTTY() || os.Getenv("NO_COLOR") != "" {
		return text
	}
	return fmt.Sprintf("\033[%sm%s\033[0m", color, text)
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%.0fm %.0fs", 
			d.Minutes(), 
			float64(int(d.Seconds())%60))
	}
	return fmt.Sprintf("%.0fh %.0fm", 
		d.Hours(), 
		float64(int(d.Minutes())%60))
}

func visibleLen(s string) int {
	// Remove ANSI escape codes for length calculation
	result := 0
	inEscape := false
	
	for _, r := range s {
		if r == '\033' {
			inEscape = true
		} else if inEscape && r == 'm' {
			inEscape = false
		} else if !inEscape {
			result++
		}
	}
	
	return result
}

func supportsANSIAndTTY() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	term := strings.TrimSpace(strings.ToLower(os.Getenv("TERM")))
	if term == "" || term == "dumb" {
		return false
	}
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}
