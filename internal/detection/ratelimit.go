package detection

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	// Embed the timezone database: Claude Code prints the zone its reset time
	// is expressed in, and hosts don't always ship /usr/share/zoneinfo.
	_ "time/tzdata"
)

// RateLimitStatus represents the rate limit state of a pane
type RateLimitStatus struct {
	IsLimited bool
	ResetsAt  string    // Original string like "2pm", "10:30am", "8m", or "1h 13m"
	ResetTime time.Time // Parsed reset time
	TimeUntil time.Duration
}

// limitPhrase matches the sentence Claude Code uses to announce a limit. The
// optional word covers the qualifier it inserts per limit type:
// "You've hit your session limit", "your weekly limit", "your 5-hour limit".
const limitPhrase = `(?:hit\s+your\s+(?:[\w-]+\s+)?limit|\blimit\s+reached\b)`

// resetGap joins the phrase to the reset time. It stays short so a stray
// "resets" elsewhere on screen can't pair with an unrelated limit message,
// but it spans newlines because a narrow pane wraps the message.
const resetGap = `.{0,80}?resets?\s+`

// Rate limit patterns - multiple formats Claude Code uses
// Examples: "limit reached ∙ resets 2pm", "limit reached ∙ resets 10:30am"
//
//	"You've hit your limit · resets 10pm (Europe/London)"
//	"You've hit your session limit · resets 6:50pm (Europe/Berlin)"
//	"Limit reached (resets 8m)" - minutes remaining format
//	"Limit reached (resets 1h 13m)" - hours and minutes remaining format
var rateLimitPatterns = []*regexp.Regexp{
	// Clock time, plus the timezone Claude Code prints beside it
	regexp.MustCompile(`(?is)` + limitPhrase + resetGap +
		`(?P<clock>\d{1,2}(?::\d{2})?\s*[ap]m)(?:\s*\((?P<tz>[A-Za-z_]+(?:/[A-Za-z_]+)*)\))?`),

	// Hours (and optionally minutes) remaining
	regexp.MustCompile(`(?is)` + limitPhrase + resetGap + `(?P<hours>\d{1,2})h(?:\s*(?P<mins>\d{1,2})m)?\b`),

	// Minutes remaining
	regexp.MustCompile(`(?is)` + limitPhrase + resetGap + `(?P<mins>\d{1,3})m\b`),
}

// Fallback patterns - detect rate limit without capturing time
// Used when we can't parse a specific reset time
// These patterns are more specific to avoid false positives
var rateLimitFallbackPatterns = []*regexp.Regexp{
	// "You've hit your limit" / "Limit reached" with no time we can read
	regexp.MustCompile(`(?i)` + limitPhrase),
	// "rate limited" as a status indicator
	regexp.MustCompile(`(?i)\brate\s+limited\b`),
}

// CheckRateLimit checks pane content for rate limit messages
func CheckRateLimit(content string) RateLimitStatus {
	content = normalizeText(content)

	// Try patterns that capture reset time first
	var matched *regexp.Regexp
	var match []string
	for _, pattern := range rateLimitPatterns {
		if match = pattern.FindStringSubmatch(content); match != nil {
			matched = pattern
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

	now := time.Now()

	// Relative format ("1h 13m", "8m"): counted from the moment we read it
	hoursStr, minutesStr := namedGroup(matched, match, "hours"), namedGroup(matched, match, "mins")
	if hoursStr != "" || minutesStr != "" {
		hours, _ := strconv.Atoi(hoursStr)     // Atoi("") returns 0
		minutes, _ := strconv.Atoi(minutesStr) // ditto

		resetsAt := minutesStr + "m"
		if hoursStr != "" {
			resetsAt = hoursStr + "h"
			if minutes > 0 {
				resetsAt += " " + minutesStr + "m"
			}
		}

		duration := time.Duration(hours)*time.Hour + time.Duration(minutes)*time.Minute
		return RateLimitStatus{
			IsLimited: true,
			ResetsAt:  resetsAt,
			ResetTime: now.Add(duration),
			TimeUntil: duration,
		}
	}

	// Clock time format (e.g., "8pm", "10:30am"), read in the zone Claude Code
	// named - it reports the account's timezone, not necessarily this host's.
	resetStr := namedGroup(matched, match, "clock")
	loc := now.Location()
	if tz := namedGroup(matched, match, "tz"); tz != "" {
		if named, err := time.LoadLocation(tz); err == nil {
			loc = named
		}
	}

	resetTime, err := parseResetTime(resetStr, loc)
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
		// Roll the calendar day rather than adding 24h: across a DST
		// transition in loc those differ by an hour, and the reset is a wall
		// clock time in the account's zone, not a fixed offset from now.
		resetTime = time.Date(resetTime.Year(), resetTime.Month(), resetTime.Day()+1,
			resetTime.Hour(), resetTime.Minute(), 0, 0, loc)
		timeUntil = resetTime.Sub(now)
	}

	return RateLimitStatus{
		IsLimited: true,
		ResetsAt:  resetStr,
		ResetTime: resetTime,
		TimeUntil: timeUntil,
	}
}

// namedGroup returns the named capture group from a match, or "" if the group
// isn't in this pattern or didn't participate in the match
func namedGroup(re *regexp.Regexp, match []string, name string) string {
	for i, n := range re.SubexpNames() {
		if n == name && i < len(match) {
			return match[i]
		}
	}
	return ""
}

// parseResetTime parses a time string like "2pm" or "10:30am" into a time.Time
// for today in loc
func parseResetTime(s string, loc *time.Location) (time.Time, error) {
	s = strings.ToLower(strings.TrimSpace(s))

	now := time.Now().In(loc)

	// Try parsing with standard layouts
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
