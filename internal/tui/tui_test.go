package tui

import (
	"testing"
	"time"

	"github.com/henryaj/autoclaude/internal/detection"
	"github.com/henryaj/autoclaude/internal/tmux"
)

func TestApplyRateLimit_PinsRelativeDeadline(t *testing.T) {
	pane := &tmux.Pane{ID: "%1"}

	// "resets 8m" is read fresh on every poll, so each reading carries a
	// deadline eight minutes from *that* poll
	first := time.Now().Add(8 * time.Minute)
	applyRateLimit(pane, detection.RateLimitStatus{IsLimited: true, ResetsAt: "8m", ResetTime: first})

	applyRateLimit(pane, detection.RateLimitStatus{
		IsLimited: true,
		ResetsAt:  "8m",
		ResetTime: first.Add(3 * time.Second),
	})

	if !pane.RateLimitTime.Equal(first) {
		t.Errorf("deadline moved from %s to %s; it must stay pinned to the first reading",
			first, pane.RateLimitTime)
	}
}

func TestApplyRateLimit_FollowsCountdown(t *testing.T) {
	pane := &tmux.Pane{ID: "%1"}
	applyRateLimit(pane, detection.RateLimitStatus{
		IsLimited: true, ResetsAt: "8m", ResetTime: time.Now().Add(8 * time.Minute),
	})

	// The message itself changed, so it's a live countdown - track it
	next := time.Now().Add(7 * time.Minute)
	applyRateLimit(pane, detection.RateLimitStatus{IsLimited: true, ResetsAt: "7m", ResetTime: next})

	if !pane.RateLimitTime.Equal(next) {
		t.Errorf("expected deadline %s, got %s", next, pane.RateLimitTime)
	}
}

func TestApplyRateLimit_RearmsForNewLimit(t *testing.T) {
	pane := &tmux.Pane{ID: "%1"}
	applyRateLimit(pane, detection.RateLimitStatus{
		IsLimited: true, ResetsAt: "2pm", ResetTime: time.Now().Add(-time.Minute),
	})
	pane.ContinueSent = true

	// A second limit later in the session, while the first message is still
	// somewhere in the scrollback
	second := time.Now().Add(2 * time.Hour)
	applyRateLimit(pane, detection.RateLimitStatus{IsLimited: true, ResetsAt: "7pm", ResetTime: second})

	if pane.ContinueSent {
		t.Error("expected ContinueSent to be re-armed for a new rate limit")
	}
	if !pane.RateLimitTime.Equal(second) {
		t.Errorf("expected deadline %s, got %s", second, pane.RateLimitTime)
	}
}

func TestApplyRateLimit_HoldsStateWhileLimited(t *testing.T) {
	pane := &tmux.Pane{ID: "%1"}
	reset := time.Now().Add(-time.Minute)
	applyRateLimit(pane, detection.RateLimitStatus{IsLimited: true, ResetsAt: "2pm", ResetTime: reset})
	pane.ContinueSent = true

	applyRateLimit(pane, detection.RateLimitStatus{IsLimited: true, ResetsAt: "2pm", ResetTime: reset})

	if !pane.ContinueSent {
		t.Error("expected ContinueSent to survive an unchanged rate limit message")
	}
}

func TestApplyRateLimit_ClearsWhenLimitGone(t *testing.T) {
	pane := &tmux.Pane{ID: "%1"}
	applyRateLimit(pane, detection.RateLimitStatus{
		IsLimited: true, ResetsAt: "2pm", ResetTime: time.Now().Add(time.Hour),
	})

	applyRateLimit(pane, detection.RateLimitStatus{IsLimited: false})

	if pane.IsRateLimited || pane.RateLimitResets != "" || !pane.RateLimitTime.IsZero() {
		t.Errorf("expected rate limit state to be cleared, got %+v", pane)
	}
}
