package detection

import (
	"testing"
)

func TestIsClaudeCode_RateLimitNew(t *testing.T) {
	content := loadFixture(t, "rate_limit_new_format.txt")
	if !IsClaudeCode(content) {
		t.Error("expected IsClaudeCode to return true for new rate limit format")
	}
}

func TestIsClaudeCode_RateLimitOld(t *testing.T) {
	content := loadFixture(t, "rate_limit_old_format.txt")
	if !IsClaudeCode(content) {
		t.Error("expected IsClaudeCode to return true for old rate limit format")
	}
}

func TestIsClaudeCode_Prompt(t *testing.T) {
	content := loadFixture(t, "claude_code_prompt.txt")
	// The prompt pattern requires box-drawing characters too
	// So a simple "> " line alone won't match
	// Let's test with box-drawing chars
	contentWithBox := "╭──────╮\n" + content
	if !IsClaudeCode(contentWithBox) {
		t.Error("expected IsClaudeCode to return true for prompt with box chars")
	}
}

func TestIsClaudeCode_NotClaudeCode(t *testing.T) {
	content := loadFixture(t, "not_claude_code.txt")
	if IsClaudeCode(content) {
		t.Error("expected IsClaudeCode to return false for non-Claude content")
	}
}

func TestIsClaudeCode_Patterns(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "limit reached message",
			content: "limit reached ∙ resets 5pm",
			want:    true,
		},
		{
			name:    "hit your limit message",
			content: "You've hit your limit · resets 10pm",
			want:    true,
		},
		{
			name:    "hit your session limit message",
			content: "You've hit your session limit",
			want:    true,
		},
		{
			name:    "footer hint",
			content: "ctrl-g to edit",
			want:    true,
		},
		{
			name:    "status bar with claude",
			content: "╭──────╮\nclaude-3-opus",
			want:    true,
		},
		{
			name:    "status bar with sonnet",
			content: "╭──────╮\nsonnet",
			want:    true,
		},
		{
			name:    "menu selector",
			content: "╭──────╮\n❯ Option 1",
			want:    true,
		},
		{
			name:    "dashed separator",
			content: "╭──────╮\n╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌",
			want:    true,
		},
		{
			name:    "plain shell",
			content: "$ echo hello\nhello",
			want:    false,
		},
		{
			name:    "empty content",
			content: "",
			want:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsClaudeCode(tc.content)
			if got != tc.want {
				t.Errorf("IsClaudeCode() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestStripANSI(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no ansi codes",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "color code",
			input: "\x1b[31mred text\x1b[0m",
			want:  "red text",
		},
		{
			name:  "multiple codes",
			input: "\x1b[1m\x1b[32mbold green\x1b[0m",
			want:  "bold green",
		},
		{
			name:  "cursor movement",
			input: "\x1b[2Jhello",
			want:  "hello",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := StripANSI(tc.input)
			if got != tc.want {
				t.Errorf("StripANSI() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGetVisibleLines(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    int // expected number of visible lines
	}{
		{
			name:    "simple lines",
			content: "line1\nline2\nline3",
			want:    3,
		},
		{
			name:    "with empty lines",
			content: "line1\n\nline2\n\n\nline3",
			want:    3,
		},
		{
			name:    "with whitespace only lines",
			content: "line1\n   \nline2\n\t\nline3",
			want:    3,
		},
		{
			name:    "with ansi codes",
			content: "\x1b[31mline1\x1b[0m\n\x1b[32mline2\x1b[0m",
			want:    2,
		},
		{
			name:    "empty content",
			content: "",
			want:    0,
		},
		{
			name:    "only empty lines",
			content: "\n\n\n",
			want:    0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := GetVisibleLines(tc.content)
			if len(got) != tc.want {
				t.Errorf("GetVisibleLines() returned %d lines, want %d", len(got), tc.want)
			}
		})
	}
}
