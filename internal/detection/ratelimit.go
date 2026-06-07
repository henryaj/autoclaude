package detection

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// RateLimitStatus represents the rate limit state of a pane
type RateLimitStatus struct {
	IsLimited  bool
	ResetsAt   string    // Original string like "2pm" or "10:30am"
	ResetTime  time.Time // Parsed reset time
	TimeUntil  time.Duration
}

// Rate limit patterns - multiple formats Claude Code uses
// Examples: "limit reached ∙ resets 2pm", "limit reached ∙ resets 10:30am"
//           "You've hit your limit · resets 10pm (Europe/London)"
//           "You've hit your session limit · resets 1:20pm (Europe/Amsterdam)"
//           "Limit reached (resets 8m)" - minutes remaining format
var rateLimitPatterns = []*regexp.Regexp{
	// New format (extra usage): "You're out of extra usage · resets 8pm (Europe/London)"
	regexp.MustCompile(`(?i)you're\s+out\s+of\s+extra\s+usage.*resets?\s+(\d{1,2}(?::\d{2})?\s*[ap]m)`),
	// New format: "You've hit your [session] limit · resets 10pm (Europe/London)"
	regexp.MustCompile(`(?i)hit\s+your\s+(?:session\s+)?limit.*resets?\s+(\d{1,2}(?::\d{2})?\s*[ap]m)`),
	// Original format: "limit reached ∙ resets 2pm"
	regexp.MustCompile(`(?i)limit\s+reached.*resets?\s+(\d{1,2}(?::\d{2})?\s*[ap]m)`),
	// Minutes remaining format: "Limit reached (resets 8m)" or "resets 45m"
	regexp.MustCompile(`(?i)(?:hit\s+your\s+(?:session\s+)?limit|limit\s+reached).*resets?\s+(\d{1,3})m\b`),
}

// Fallback patterns - detect rate limit without capturing time
// Used when we can't parse a specific reset time
// These patterns are more specific to avoid false positives
var rateLimitFallbackPatterns = []*regexp.Regexp{
	// "You've hit your limit" - Claude Code's primary message
	regexp.MustCompile(`(?i)you['']ve\s+hit\s+your\s+limit`),
	// "You've hit your session limit"
	regexp.MustCompile(`(?i)you['']ve\s+hit\s+your\s+session\s+limit`),
	// "Limit reached" at word boundary (not "rate limit exceeded" or similar)
	regexp.MustCompile(`(?i)\blimit\s+reached\b`),
	// "rate limited" as a status indicator
	regexp.MustCompile(`(?i)\brate\s+limited\b`),
}

// CheckRateLimit checks pane content for rate limit messages
func CheckRateLimit(content string) RateLimitStatus {
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
				// Rate limited but couldn't parse time - return with empty ResetsAt
				return RateLimitStatus{
					IsLimited: true,
					ResetsAt:  "", // Unknown reset time
				}
			}
		}
		return RateLimitStatus{IsLimited: false}
	}

	resetStr := match[1]
	now := time.Now()

	// Last pattern is the minutes-remaining format (e.g., "8m" -> "8")
	if patternIdx == len(rateLimitPatterns)-1 {
		minutes, err := strconv.Atoi(resetStr)
		if err != nil {
			return RateLimitStatus{
				IsLimited: true,
				ResetsAt:  resetStr + "m",
			}
		}
		resetTime := now.Add(time.Duration(minutes) * time.Minute)
		return RateLimitStatus{
			IsLimited: true,
			ResetsAt:  resetStr + "m",
			ResetTime: resetTime,
			TimeUntil: time.Duration(minutes) * time.Minute,
		}
	}

	// Clock time format (e.g., "8pm", "10:30am")
	resetTime, err := parseResetTime(resetStr)
	if err != nil {
		// Pattern matched but couldn't parse time - still rate limited
		return RateLimitStatus{
			IsLimited: true,
			ResetsAt:  resetStr,
		}
	}

	timeUntil := resetTime.Sub(now)

	// If the time is more than 1 hour in the past, it's likely for tomorrow.
	// But if it's within the last hour, keep it as-is so we can detect
	// that the reset time has passed and trigger the continue action.
	if timeUntil < -1*time.Hour {
		resetTime = resetTime.Add(24 * time.Hour)
		timeUntil = resetTime.Sub(now)
	}

	return RateLimitStatus{
		IsLimited: true,
		ResetsAt:  resetStr,
		ResetTime: resetTime,
		TimeUntil: timeUntil,
	}
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
