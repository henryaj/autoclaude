package tmux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

var (
	ErrNotInTmux = errors.New("not running inside tmux (TMUX environment variable not set)")
	ErrTimeout   = errors.New("tmux command timed out")
)

const commandTimeout = 5 * time.Second

// CheckTmuxEnv validates that we're running inside tmux
func CheckTmuxEnv() error {
	if os.Getenv("TMUX") == "" {
		return ErrNotInTmux
	}
	return nil
}

// CurrentPaneID returns the ID of the pane where this process is running
func CurrentPaneID() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "tmux", "display-message", "-p", "#{pane_id}")
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", ErrTimeout
		}
		return "", fmt.Errorf("tmux display-message: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// CurrentWindowID returns the ID of the window where this process is running
func CurrentWindowID() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "tmux", "display-message", "-p", "#{window_id}")
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", ErrTimeout
		}
		return "", fmt.Errorf("tmux display-message: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// ListPanes returns the layout of panes in the specified window (or current window if empty)
func ListPanes(windowID string) (*Layout, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	// Format: pane_id pane_left pane_top pane_width pane_height pane_title
	args := []string{"list-panes", "-F", "#{pane_id} #{pane_left} #{pane_top} #{pane_width} #{pane_height} #{pane_title}"}
	if windowID != "" {
		args = append(args, "-t", windowID)
	}
	cmd := exec.CommandContext(ctx, "tmux", args...)
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, ErrTimeout
		}
		return nil, fmt.Errorf("tmux list-panes: %w", err)
	}

	return parseListPanes(string(output))
}

// parseListPanes parses the output of tmux list-panes
func parseListPanes(output string) (*Layout, error) {
	layout := &Layout{
		Panes: make([]*Pane, 0),
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		pane, err := parsePaneLine(line)
		if err != nil {
			return nil, fmt.Errorf("parse pane line %q: %w", line, err)
		}
		layout.Panes = append(layout.Panes, pane)
	}

	return layout, nil
}

// parsePaneLine parses a single line of tmux list-panes output
func parsePaneLine(line string) (*Pane, error) {
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return nil, fmt.Errorf("expected at least 5 fields, got %d", len(fields))
	}

	left, err := strconv.Atoi(fields[1])
	if err != nil {
		return nil, fmt.Errorf("parse left: %w", err)
	}

	top, err := strconv.Atoi(fields[2])
	if err != nil {
		return nil, fmt.Errorf("parse top: %w", err)
	}

	width, err := strconv.Atoi(fields[3])
	if err != nil {
		return nil, fmt.Errorf("parse width: %w", err)
	}

	height, err := strconv.Atoi(fields[4])
	if err != nil {
		return nil, fmt.Errorf("parse height: %w", err)
	}

	// Title may contain spaces, so join remaining fields
	title := ""
	if len(fields) >= 6 {
		title = strings.Join(fields[5:], " ")
	}

	return &Pane{
		ID:      fields[0],
		Left:    left,
		Top:     top,
		Width:   width,
		Height:  height,
		Title:   title,
		Mode:    ModeOff,
	}, nil
}

// CapturePane captures the visible content of a pane
func CapturePane(paneID string) (string, error) {
	return capturePane(paneID, 0)
}

// CapturePaneRecent captures the last n lines of scrollback plus the visible screen.
// Useful when rate-limit messages scroll off the visible area but the options menu remains.
func CapturePaneRecent(paneID string, lines int) (string, error) {
	if lines <= 0 {
		return CapturePane(paneID)
	}
	return capturePane(paneID, -lines)
}

func capturePane(paneID string, startLine int) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	args := []string{"capture-pane", "-t", paneID, "-p"}
	if startLine != 0 {
		args = append(args, "-S", strconv.Itoa(startLine))
	}
	cmd := exec.CommandContext(ctx, "tmux", args...)
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", ErrTimeout
		}
		return "", fmt.Errorf("tmux capture-pane: %w", err)
	}

	return string(output), nil
}

// SendKeys sends keystrokes to a pane
func SendKeys(paneID string, keys ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	args := []string{"send-keys", "-t", paneID}
	args = append(args, keys...)

	cmd := exec.CommandContext(ctx, "tmux", args...)
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return ErrTimeout
		}
		return fmt.Errorf("tmux send-keys: %w", err)
	}

	return nil
}
