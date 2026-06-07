package detection

import (
	"regexp"
	"strings"
)

// Claude Code UI patterns - multiple approaches for robustness
var (
	// Box-drawing characters used in Claude Code's UI
	boxDrawingPattern = regexp.MustCompile(`[─│┌┐└┘├┤┬┴┼╭╮╯╰]`)

	// The input prompt pattern: > at start of line (with possible ANSI codes)
	promptPattern = regexp.MustCompile(`(?m)^(\x1b\[[0-9;]*m)*>\s`)

	// Claude Code status bar patterns (model names, etc.)
	statusBarPattern = regexp.MustCompile(`(?i)(claude|anthropic|sonnet|opus|haiku)`)

	// Footer hint that appears in both prompt and menu modes
	footerHintPattern = regexp.MustCompile(`ctrl-g to edit`)

	// Menu selector used in question/choice UI
	menuSelectorPattern = regexp.MustCompile(`❯`)

	// Rate limit messages - definitive proof it's Claude Code
	rateLimitMsgPattern    = regexp.MustCompile(`(?i)limit\s+reached`)
	rateLimitMsgPatternAlt = regexp.MustCompile(`(?i)hit\s+your\s+limit`)
	rateLimitMsgPatternAlt2 = regexp.MustCompile(`(?i)You're\s+out\s+of\s+extra\s+usage[ ·]+resets`)
	rateLimitMsgPatternAlt3 = regexp.MustCompile(`(?i)hit\s+your\s+session\s+limit`)

	// Dashed separator line used in Claude Code UI
	dashedSeparator = regexp.MustCompile(`╌{10,}`)
)

// IsClaudeCode detects if pane content appears to be running Claude Code
func IsClaudeCode(content string) bool {
	// Rate limit message is definitive - if we see it, it's Claude Code
	if rateLimitMsgPattern.MatchString(content) || rateLimitMsgPatternAlt.MatchString(content) || rateLimitMsgPatternAlt2.MatchString(content) || rateLimitMsgPatternAlt3.MatchString(content) {
		return true
	}

	// Footer hint is very reliable
	if footerHintPattern.MatchString(content) {
		return true
	}

	// Must have box-drawing characters for other detection methods
	if !boxDrawingPattern.MatchString(content) {
		return false
	}

	// Any of these patterns indicate Claude Code
	if promptPattern.MatchString(content) {
		return true
	}
	if statusBarPattern.MatchString(content) {
		return true
	}
	if menuSelectorPattern.MatchString(content) {
		return true
	}
	if dashedSeparator.MatchString(content) {
		return true
	}

	return false
}

// StripANSI removes ANSI escape codes from a string
func StripANSI(s string) string {
	ansiPattern := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	return ansiPattern.ReplaceAllString(s, "")
}

// GetVisibleLines returns non-empty lines from content
func GetVisibleLines(content string) []string {
	lines := strings.Split(content, "\n")
	visible := make([]string, 0, len(lines))
	for _, line := range lines {
		stripped := strings.TrimSpace(StripANSI(line))
		if stripped != "" {
			visible = append(visible, stripped)
		}
	}
	return visible
}
