package tmux

import "time"

// PaneMode represents the operating mode for a pane
type PaneMode int

const (
	ModeOff PaneMode = iota
	ModeContinueOnRateLimit
)

func (m PaneMode) String() string {
	switch m {
	case ModeContinueOnRateLimit:
		return "auto"
	default:
		return "off"
	}
}

// Direction represents a spatial direction for navigation
type Direction int

const (
	DirLeft Direction = iota
	DirRight
	DirUp
	DirDown
)

// Pane represents a tmux pane with its position and state
type Pane struct {
	ID                    string
	Left, Top             int
	Width, Height         int
	Title                 string // Pane title (set by running application)
	Mode                  PaneMode
	HasClaudeCode         bool
	IsRateLimited         bool
	RateLimitResets       string    // Display string like "10pm" or empty if unknown
	RateLimitTime         time.Time // Parsed reset time (zero if unknown)
	ContinueSent          bool      // Whether we've sent continue for this rate limit
	LastPeriodicContinue  time.Time // For unknown reset times: when we last sent periodic continue
	RateLimitMenuOpen     bool      // /rate-limit-options menu is on screen
	RateLimitMenuConfirmed bool     // Auto-confirmed "Stop and wait for limit to reset"
}

// Center returns the center point of the pane
func (p *Pane) Center() (x, y int) {
	return p.Left + p.Width/2, p.Top + p.Height/2
}

// Layout represents the tmux window layout
type Layout struct {
	Panes []*Pane
}

// PaneByID finds a pane by its ID
func (l *Layout) PaneByID(id string) *Pane {
	for _, p := range l.Panes {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// PaneInDirection finds the nearest pane in the given direction from current
func (l *Layout) PaneInDirection(current *Pane, dir Direction) *Pane {
	if current == nil || len(l.Panes) == 0 {
		return nil
	}

	cx, cy := current.Center()
	var best *Pane
	bestDist := -1

	for _, p := range l.Panes {
		if p.ID == current.ID {
			continue
		}

		px, py := p.Center()
		dx, dy := px-cx, py-cy

		// Check if pane is in the correct direction
		inDirection := false
		switch dir {
		case DirLeft:
			inDirection = dx < 0 && abs(dx) > abs(dy)
		case DirRight:
			inDirection = dx > 0 && abs(dx) > abs(dy)
		case DirUp:
			inDirection = dy < 0 && abs(dy) > abs(dx)
		case DirDown:
			inDirection = dy > 0 && abs(dy) > abs(dx)
		}

		if !inDirection {
			continue
		}

		// Calculate distance (Manhattan distance)
		dist := abs(dx) + abs(dy)
		if best == nil || dist < bestDist {
			best = p
			bestDist = dist
		}
	}

	return best
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
