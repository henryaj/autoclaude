package detection

import (
	"os"
	"path/filepath"
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
			name:     "session limit with minutes",
			content:  "You've hit your session limit · resets 1:20pm (Europe/Amsterdam)\n/upgrade to increase your usage limit.",
			wantTime: "1:20pm",
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
			content: "You've hit your limit",
		},
		{
			name:    "hit your session limit",
			content: "You've hit your session limit",
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
