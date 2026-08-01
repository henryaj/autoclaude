package detection

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// RateLimitStatus represents the rate limit state of a pane
type RateLimitStatus struct {
	IsLimited           bool
	ResetsAt            string    // Original string like "2pm" or "10:30am"
	ResetTime           time.Time // Parsed reset time
	TimeUntil           time.Duration
	OptionsMenuOpen     bool // /rate-limit-options "What do you want to do?" menu
	HighlightedOption   int  // 1-based menu option with ❯ (0 if unknown)
	WaitOptionSelected  bool // ❯ is on "Stop and wait for limit to reset"
}

// hitYourLimit matches "hit your limit", "hit your session limit", "hit your weekly limit"
const hitYourLimitPattern = `hit\s+your\s+(?:(?:session|weekly)\s+)?limit`

// Rate limit patterns - multiple formats Claude Code uses
// Examples: "limit reached ∙ resets 2pm", "limit reached ∙ resets 10:30am"
//           "You've hit your limit · resets 10pm (Europe/London)"
//           "You've hit your session limit · resets 1:20pm (Asia/Jerusalem)"
//           "Limit reached (resets 8m)" - minutes remaining format
var rateLimitPatterns = []*regexp.Regexp{
	// "You've hit your [session|weekly] limit · resets 10pm (Europe/London)"
	regexp.MustCompile(`(?i)` + hitYourLimitPattern + `.*resets?\s+(\d{1,2}(?::\d{2})?\s*[ap]m)`),
	// Original format: "limit reached ∙ resets 2pm"
	regexp.MustCompile(`(?i)limit\s+reached.*resets?\s+(\d{1,2}(?::\d{2})?\s*[ap]m)`),
	// Minutes remaining format: "Limit reached (resets 8m)" or "resets 45m"
	regexp.MustCompile(`(?i)(?:` + hitYourLimitPattern + `|limit\s+reached).*resets?\s+(\d{1,3})m\b`),
}

// Fallback patterns - detect rate limit without capturing time
// Used when we can't parse a specific reset time
// These patterns are more specific to avoid false positives
var rateLimitFallbackPatterns = []*regexp.Regexp{
	// "You've hit your [session|weekly] limit" - Claude Code's primary messages
	regexp.MustCompile(`(?i)you['']ve\s+` + hitYourLimitPattern),
	// "Limit reached" at word boundary (not "rate limit exceeded" or similar)
	regexp.MustCompile(`(?i)\blimit\s+reached\b`),
	// "rate limited" as a status indicator
	regexp.MustCompile(`(?i)\brate\s+limited\b`),
}

// Interactive /rate-limit-options menu (often shown without a visible reset time)
var (
	rateLimitMenuPattern      = regexp.MustCompile(`(?i)(/rate-limit-options|what do you want to do)`)
	rateLimitMenuStopWaitLine = regexp.MustCompile(`(?i)stop\s+and\s+wait.*limit`)
	menuHighlightedOption     = regexp.MustCompile(`❯\s*(\d+)\.\s*(.+)$`)
)

func parseRateLimitMenu(content string) (open bool, highlighted int, waitSelected bool) {
	if !rateLimitMenuPattern.MatchString(content) {
		return false, 0, false
	}

	open = true
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(StripANSI(line))
		if line == "" {
			continue
		}
		m := menuHighlightedOption.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		opt, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		highlighted = opt
		if rateLimitMenuStopWaitLine.MatchString(m[2]) {
			waitSelected = true
		}
		break
	}

	// Menu visible but selector not captured (narrow pane, ANSI quirks)
	if highlighted == 0 && rateLimitMenuStopWaitLine.MatchString(content) {
		waitSelected = true
	}

	return open, highlighted, waitSelected
}

// CheckRateLimit checks pane content for rate limit messages
func CheckRateLimit(content string) RateLimitStatus {
	content = normalizeRateLimitContent(content)
	// Try patterns that capture reset time first
	var match []string
	var patternIdx int
	for i, pattern := range rateLimitPatterns {
		match = pattern.FindStringSubmatch(content)
		if match != nil {
			patternIdx = i
			break
		}
	}

	// If no time-capturing pattern matched, try fallback patterns
	if match == nil {
		for _, pattern := range rateLimitFallbackPatterns {
			if pattern.MatchString(content) {
				status := RateLimitStatus{
					IsLimited: true,
					ResetsAt:  "", // Unknown reset time
				}
				applyMenuStatus(&status, content)
				return status
			}
		}

		menuOpen, highlighted, waitSelected := parseRateLimitMenu(content)
		if menuOpen {
			return RateLimitStatus{
				IsLimited:          true,
				ResetsAt:           "",
				OptionsMenuOpen:    true,
				HighlightedOption:  highlighted,
				WaitOptionSelected: waitSelected,
			}
		}

		return RateLimitStatus{IsLimited: false}
	}

	resetStr := match[1]
	now := time.Now()

	// Pattern index 2 is the minutes-remaining format (e.g., "8m" -> "8")
	if patternIdx == 2 {
		minutes, err := strconv.Atoi(resetStr)
		if err != nil {
			status := RateLimitStatus{
				IsLimited: true,
				ResetsAt:  resetStr + "m",
			}
			applyMenuStatus(&status, content)
			return status
		}
		resetTime := now.Add(time.Duration(minutes) * time.Minute)
		status := RateLimitStatus{
			IsLimited: true,
			ResetsAt:  resetStr + "m",
			ResetTime: resetTime,
			TimeUntil: time.Duration(minutes) * time.Minute,
		}
		applyMenuStatus(&status, content)
		return status
	}

	// Clock time format (e.g., "8pm", "10:30am")
	resetTime, err := parseResetTime(resetStr)
	if err != nil {
		// Pattern matched but couldn't parse time - still rate limited
		status := RateLimitStatus{
			IsLimited: true,
			ResetsAt:  resetStr,
		}
		applyMenuStatus(&status, content)
		return status
	}

	timeUntil := resetTime.Sub(now)

	// If the time is more than 1 hour in the past, it's likely for tomorrow.
	// But if it's within the last hour, keep it as-is so we can detect
	// that the reset time has passed and trigger the continue action.
	if timeUntil < -1*time.Hour {
		resetTime = resetTime.Add(24 * time.Hour)
		timeUntil = resetTime.Sub(now)
	}

	status := RateLimitStatus{
		IsLimited: true,
		ResetsAt:  resetStr,
		ResetTime: resetTime,
		TimeUntil: timeUntil,
	}
	applyMenuStatus(&status, content)
	return status
}

func applyMenuStatus(status *RateLimitStatus, content string) {
	menuOpen, highlighted, waitSelected := parseRateLimitMenu(content)
	status.OptionsMenuOpen = menuOpen
	status.HighlightedOption = highlighted
	status.WaitOptionSelected = waitSelected
}

// parseResetTime parses a time string like "2pm" or "10:30am" into a time.Time for today
func parseResetTime(s string) (time.Time, error) {
	s = strings.ToLower(strings.TrimSpace(s))

	now := time.Now()
	loc := now.Location()

	// Try parsing with minutes first: "10:30am"
	formats := []string{
		"3:04pm",
		"3:04 pm",
		"3pm",
		"3 pm",
	}

	for _, format := range formats {
		t, err := time.ParseInLocation(format, s, loc)
		if err == nil {
			// Combine parsed time with today's date
			return time.Date(now.Year(), now.Month(), now.Day(),
				t.Hour(), t.Minute(), 0, 0, loc), nil
		}
	}

	// Manual parsing as fallback
	isPM := strings.Contains(s, "pm")
	s = strings.ReplaceAll(s, "am", "")
	s = strings.ReplaceAll(s, "pm", "")
	s = strings.TrimSpace(s)

	var hour, minute int
	if strings.Contains(s, ":") {
		parts := strings.Split(s, ":")
		hour, _ = strconv.Atoi(parts[0])
		minute, _ = strconv.Atoi(parts[1])
	} else {
		hour, _ = strconv.Atoi(s)
		minute = 0
	}

	// Convert to 24-hour format
	if isPM && hour != 12 {
		hour += 12
	} else if !isPM && hour == 12 {
		hour = 0
	}

	return time.Date(now.Year(), now.Month(), now.Day(),
		hour, minute, 0, 0, loc), nil
}

// HasReset checks if the rate limit has reset (time has passed)
func (r RateLimitStatus) HasReset() bool {
	if !r.IsLimited {
		return false
	}
	if r.ResetTime.IsZero() {
		return false
	}
	return time.Now().After(r.ResetTime)
}

// normalizeRateLimitContent fixes common terminal encoding glitches in Claude Code output.
func normalizeRateLimitContent(s string) string {
	replacements := []string{
		"Â·", "·",
		"âˆ™", "·",
		"∙", "·",
	}
	for i := 0; i < len(replacements); i += 2 {
		s = strings.ReplaceAll(s, replacements[i], replacements[i+1])
	}
	return s
}
