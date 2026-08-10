package detection

import (
	"regexp"
	"strings"
)

// Claude Code UI patterns - multiple approaches for robustness
var (
	// Frame characters Claude Code draws its UI with: box-drawing for the
	// input border, an upper eighth block for the rule above dialogs
	uiFramePattern = regexp.MustCompile(`[─│┌┐└┘├┤┬┴┼╭╮╯╰▔]`)

	// The input prompt pattern: > at start of line (with possible ANSI codes)
	promptPattern = regexp.MustCompile(`(?m)^(\x1b\[[0-9;]*m)*>\s`)

	// Claude Code status bar patterns (model names, etc.)
	statusBarPattern = regexp.MustCompile(`(?i)(claude|anthropic|sonnet|opus|haiku)`)

	// Footer hint that appears in both prompt and menu modes
	footerHintPattern = regexp.MustCompile(`ctrl-g to edit`)

	// Menu selector used in question/choice UI
	menuSelectorPattern = regexp.MustCompile(`❯`)

	// Rate limit message - definitive proof it's Claude Code
	rateLimitMsgPattern = regexp.MustCompile(`(?i)` + limitPhrase)

	// Dashed separator line used in Claude Code UI
	dashedSeparator = regexp.MustCompile(`╌{10,}`)
)

// IsClaudeCode detects if pane content appears to be running Claude Code
func IsClaudeCode(content string) bool {
	content = normalizeText(content)

	// Rate limit message is definitive - if we see it, it's Claude Code
	if rateLimitMsgPattern.MatchString(content) {
		return true
	}

	// Footer hint is very reliable
	if footerHintPattern.MatchString(content) {
		return true
	}

	// Must have frame characters for other detection methods
	if !uiFramePattern.MatchString(content) {
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

// textNormalizer folds the typography Claude Code renders with back to ASCII.
// It pads output with non-breaking spaces and curls apostrophes, neither of
// which Go's ASCII-only \s and literal ' can match.
var textNormalizer = strings.NewReplacer(
	"\u00a0", " ", // no-break space
	"\u202f", " ", // narrow no-break space
	"\u2009", " ", // thin space
	"\u2019", "'", // right single quotation mark
)

// normalizeText prepares pane content for pattern matching
func normalizeText(s string) string {
	return textNormalizer.Replace(s)
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
