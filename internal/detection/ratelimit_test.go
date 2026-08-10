package detection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func loadFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("failed to load fixture %s: %v", name, err)
	}
	return string(data)
}

func TestCheckRateLimit_NewFormat(t *testing.T) {
	content := loadFixture(t, "rate_limit_new_format.txt")
	status := CheckRateLimit(content)

	if !status.IsLimited {
		t.Error("expected IsLimited to be true")
	}
	if status.ResetsAt != "10pm" {
		t.Errorf("expected ResetsAt to be '10pm', got '%s'", status.ResetsAt)
	}
	if status.ResetTime.IsZero() {
		t.Error("expected ResetTime to be set")
	}
}

func TestCheckRateLimit_OldFormat(t *testing.T) {
	content := loadFixture(t, "rate_limit_old_format.txt")
	status := CheckRateLimit(content)

	if !status.IsLimited {
		t.Error("expected IsLimited to be true")
	}
	if status.ResetsAt != "2pm" {
		t.Errorf("expected ResetsAt to be '2pm', got '%s'", status.ResetsAt)
	}
	if status.ResetTime.IsZero() {
		t.Error("expected ResetTime to be set")
	}
}

func TestCheckRateLimit_NoMatch(t *testing.T) {
	content := loadFixture(t, "not_claude_code.txt")
	status := CheckRateLimit(content)

	if status.IsLimited {
		t.Error("expected IsLimited to be false")
	}
}

func TestCheckRateLimit_TimeFormats(t *testing.T) {
	cases := []struct {
		name     string
		content  string
		wantTime string
	}{
		{
			name:     "simple pm",
			content:  "You've hit your limit · resets 2pm",
			wantTime: "2pm",
		},
		{
			name:     "simple am",
			content:  "You've hit your limit · resets 9am",
			wantTime: "9am",
		},
		{
			name:     "with minutes",
			content:  "limit reached ∙ resets 10:30am",
			wantTime: "10:30am",
		},
		{
			name:     "with space before am/pm",
			content:  "limit reached ∙ resets 3 pm",
			wantTime: "3 pm",
		},
		{
			name:     "double digit hour",
			content:  "You've hit your limit · resets 11pm (Europe/London)",
			wantTime: "11pm",
		},
		{
			name:     "minutes remaining format",
			content:  "⚠ Limit reached (resets 8m)",
			wantTime: "8m",
		},
		{
			name:     "minutes remaining double digit",
			content:  "Limit reached (resets 45m)",
			wantTime: "45m",
		},
		{
			name:     "minutes remaining triple digit",
			content:  "⚠ Limit reached (resets 120m)",
			wantTime: "120m",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status := CheckRateLimit(tc.content)
			if !status.IsLimited {
				t.Error("expected IsLimited to be true")
			}
			if status.ResetsAt != tc.wantTime {
				t.Errorf("expected ResetsAt to be '%s', got '%s'", tc.wantTime, status.ResetsAt)
			}
		})
	}
}

func TestCheckRateLimit_MinutesFormat(t *testing.T) {
	status := CheckRateLimit("⚠ Limit reached (resets 30m)")

	if !status.IsLimited {
		t.Error("expected IsLimited to be true")
	}
	if status.ResetsAt != "30m" {
		t.Errorf("expected ResetsAt to be '30m', got '%s'", status.ResetsAt)
	}
	if status.ResetTime.IsZero() {
		t.Error("expected ResetTime to be set")
	}
	// TimeUntil should be approximately 30 minutes (within 1 second tolerance)
	expectedDuration := 30 * time.Minute
	if status.TimeUntil < expectedDuration-time.Second || status.TimeUntil > expectedDuration+time.Second {
		t.Errorf("expected TimeUntil to be ~30m, got %v", status.TimeUntil)
	}
}

func TestCheckRateLimit_FallbackNoTime(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{
			name:    "hit your limit without time",
			content: "You've hit your limit",
		},
		{
			name:    "hit your limit with curly apostrophe",
			content: "You\u2019ve hit your limit",
		},
		{
			name:    "limit reached without time",
			content: "Limit reached - please wait",
		},
		{
			name:    "rate limited status",
			content: "⚠ Rate limited",
		},
		{
			name:    "limit reached with unparseable time format",
			content: "Limit reached (resets in 2 hours)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status := CheckRateLimit(tc.content)
			if !status.IsLimited {
				t.Error("expected IsLimited to be true")
			}
			if status.ResetsAt != "" {
				t.Errorf("expected ResetsAt to be empty for fallback, got '%s'", status.ResetsAt)
			}
			if !status.ResetTime.IsZero() {
				t.Error("expected ResetTime to be zero for fallback")
			}
		})
	}
}

func TestCheckRateLimit_NoMatchCases(t *testing.T) {
	cases := []string{
		"Normal output without rate limit",
		"The limit of my patience",
		"Rate your experience",
	}

	for _, content := range cases {
		t.Run(content, func(t *testing.T) {
			status := CheckRateLimit(content)
			if status.IsLimited {
				t.Errorf("expected IsLimited to be false for: %q", content)
			}
		})
	}
}

func TestHasReset(t *testing.T) {
	now := time.Now()

	cases := []struct {
		name   string
		status RateLimitStatus
		want   bool
	}{
		{
			name:   "not limited",
			status: RateLimitStatus{IsLimited: false},
			want:   false,
		},
		{
			name:   "limited but no reset time",
			status: RateLimitStatus{IsLimited: true},
			want:   false,
		},
		{
			name: "limited, reset time in future",
			status: RateLimitStatus{
				IsLimited: true,
				ResetTime: now.Add(1 * time.Hour),
			},
			want: false,
		},
		{
			name: "limited, reset time in past",
			status: RateLimitStatus{
				IsLimited: true,
				ResetTime: now.Add(-1 * time.Hour),
			},
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.status.HasReset()
			if got != tc.want {
				t.Errorf("HasReset() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCheckRateLimit_SessionLimit(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatalf("failed to load Europe/Berlin: %v", err)
	}

	// Both captures are the same screen; the dialog is only present until the
	// user dismisses it
	for _, name := range []string{"session_limit.txt", "session_limit_dialog.txt"} {
		t.Run(name, func(t *testing.T) {
			status := CheckRateLimit(loadFixture(t, name))

			if !status.IsLimited {
				t.Fatal("expected IsLimited to be true")
			}
			if status.ResetsAt != "6:50pm" {
				t.Errorf("expected ResetsAt to be '6:50pm', got '%s'", status.ResetsAt)
			}
			if got := status.ResetTime.In(berlin); got.Hour() != 18 || got.Minute() != 50 {
				t.Errorf("expected reset at 18:50 Europe/Berlin, got %s", got)
			}
		})
	}
}

func TestCheckRateLimit_ResetTimezone(t *testing.T) {
	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatalf("failed to load Europe/London: %v", err)
	}

	// The zone Claude Code prints is the account's, which need not match this
	// host's - "11pm (Europe/London)" is 11pm in London wherever we're running
	status := CheckRateLimit("You've hit your limit \u00b7 resets 11pm (Europe/London)")

	if !status.IsLimited {
		t.Fatal("expected IsLimited to be true")
	}
	if got := status.ResetTime.In(london); got.Hour() != 23 || got.Minute() != 0 {
		t.Errorf("expected reset at 23:00 Europe/London, got %s", got)
	}
}

func TestCheckRateLimit_UnknownTimezoneFallsBackToLocal(t *testing.T) {
	status := CheckRateLimit("You've hit your limit \u00b7 resets 11pm (Middle/Earth)")

	if !status.IsLimited {
		t.Fatal("expected IsLimited to be true")
	}
	if got := status.ResetTime.In(time.Now().Location()); got.Hour() != 23 {
		t.Errorf("expected reset at 23:00 local time, got %s", got)
	}
}

func TestCheckRateLimit_HoursFormat(t *testing.T) {
	cases := []struct {
		name         string
		content      string
		wantResetsAt string
		wantUntil    time.Duration
	}{
		{
			name:         "hours and minutes",
			content:      "\u26a0 Limit reached (resets 1h 13m)",
			wantResetsAt: "1h 13m",
			wantUntil:    73 * time.Minute,
		},
		{
			name:         "whole hours",
			content:      "\u26a0 Limit reached (resets 2h)",
			wantResetsAt: "2h",
			wantUntil:    2 * time.Hour,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status := CheckRateLimit(tc.content)

			if !status.IsLimited {
				t.Fatal("expected IsLimited to be true")
			}
			if status.ResetsAt != tc.wantResetsAt {
				t.Errorf("expected ResetsAt to be '%s', got '%s'", tc.wantResetsAt, status.ResetsAt)
			}
			if status.TimeUntil < tc.wantUntil-time.Second || status.TimeUntil > tc.wantUntil+time.Second {
				t.Errorf("expected TimeUntil to be ~%v, got %v", tc.wantUntil, status.TimeUntil)
			}
		})
	}
}

func TestCheckRateLimit_TypographyAndWrapping(t *testing.T) {
	cases := []struct {
		name     string
		content  string
		wantTime string
	}{
		{
			name:     "session qualifier",
			content:  "You've hit your session limit \u00b7 resets 6:50pm (Europe/Berlin)",
			wantTime: "6:50pm",
		},
		{
			name:     "weekly qualifier",
			content:  "You've hit your weekly limit \u00b7 resets 9am",
			wantTime: "9am",
		},
		{
			name:     "non-breaking spaces",
			content:  "\u00a0You've hit your\u00a0session limit \u00b7 resets\u00a02pm",
			wantTime: "2pm",
		},
		{
			name:     "curly apostrophe",
			content:  "You\u2019ve hit your session limit \u00b7 resets 2pm",
			wantTime: "2pm",
		},
		{
			name:     "message wrapped across lines in a narrow pane",
			content:  "  \u23bf  You've hit your session\n     limit \u00b7 resets 6:50pm\n     (Europe/Berlin)",
			wantTime: "6:50pm",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status := CheckRateLimit(tc.content)

			if !status.IsLimited {
				t.Fatal("expected IsLimited to be true")
			}
			if status.ResetsAt != tc.wantTime {
				t.Errorf("expected ResetsAt to be '%s', got '%s'", tc.wantTime, status.ResetsAt)
			}
			if status.ResetTime.IsZero() {
				t.Error("expected ResetTime to be set")
			}
		})
	}
}

func TestCheckRateLimit_UnrelatedResetsNearby(t *testing.T) {
	// The limit message and a "resets" line far apart on screen must not be
	// stitched together into a reset time
	content := "Limit reached\n" + strings.Repeat("filler output\n", 10) + "the cache resets 4pm"

	status := CheckRateLimit(content)

	if !status.IsLimited {
		t.Fatal("expected IsLimited to be true")
	}
	if status.ResetsAt != "" {
		t.Errorf("expected ResetsAt to be empty, got '%s'", status.ResetsAt)
	}
}
