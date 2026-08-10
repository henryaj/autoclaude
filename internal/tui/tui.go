package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/henryaj/autoclaude/internal/detection"
	"github.com/henryaj/autoclaude/internal/tmux"
)

const pollInterval = 3 * time.Second

// Colors - bold, high-contrast palette
var (
	accentCyan   = lipgloss.Color("#00ffff") // Bright cyan
	accentPurple = lipgloss.Color("#bd93f9") // Soft purple
	brightWhite  = lipgloss.Color("#f8f8f2") // Off-white
	mutedGray    = lipgloss.Color("#6272a4") // Muted blue-gray
	borderColor  = lipgloss.Color("#44475a") // Dark purple-gray
)

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(accentCyan)

	versionStyle = lipgloss.NewStyle().
			Foreground(accentPurple)

	headerStyle = lipgloss.NewStyle().
			PaddingLeft(1).
			MarginBottom(1)

	mainPaneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Padding(1, 2)

	dimTextStyle = lipgloss.NewStyle().
			Foreground(mutedGray)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ff5555")).
			Bold(true)
)

// Messages
type layoutUpdateMsg struct {
	layout *tmux.Layout
	err    error
}

type pollTickMsg time.Time

type initMsg struct {
	ownPaneID   string
	ownWindowID string
	layout      *tmux.Layout
	err         error
}

type Model struct {
	version          string
	width            int
	height           int
	layout           *tmux.Layout
	selectedPaneID   string
	ownPaneID        string // The pane running autoclaude (excluded from detection)
	ownWindowID      string // The window to monitor (pinned at startup)
	err              error
	errTime          time.Time // When the error occurred (for auto-clear)
	testPattern      string    // Test mode: trigger on this string instead of rate limit
	lastContinueSent time.Time // When we last sent a continue command
	lastContinuePane string    // Which pane we sent it to
	showHelp         bool      // Whether to show the help overlay
}

func New(version string, testPattern string) Model {
	return Model{
		version:     version,
		testPattern: testPattern,
		width:       80,
		height:      24,
	}
}

func (m Model) Init() tea.Cmd {
	return doInit
}

func doInit() tea.Msg {
	ownPaneID, err := tmux.CurrentPaneID()
	if err != nil {
		return initMsg{err: err}
	}

	ownWindowID, err := tmux.CurrentWindowID()
	if err != nil {
		return initMsg{ownPaneID: ownPaneID, err: err}
	}

	layout, err := tmux.ListPanes(ownWindowID)
	if err != nil {
		return initMsg{ownPaneID: ownPaneID, ownWindowID: ownWindowID, err: err}
	}

	return initMsg{ownPaneID: ownPaneID, ownWindowID: ownWindowID, layout: layout}
}

func tickCmd() tea.Cmd {
	return tea.Tick(pollInterval, func(t time.Time) tea.Msg {
		return pollTickMsg(t)
	})
}

func fetchLayoutCmd(windowID string) tea.Cmd {
	return func() tea.Msg {
		layout, err := tmux.ListPanes(windowID)
		return layoutUpdateMsg{layout: layout, err: err}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// If help is shown, any key dismisses it
		if m.showHelp {
			m.showHelp = false
			return m, nil
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "h", "?":
			m.showHelp = true
		case "left":
			m.moveSelection(tmux.DirLeft)
		case "right":
			m.moveSelection(tmux.DirRight)
		case "up":
			m.moveSelection(tmux.DirUp)
		case "down":
			m.moveSelection(tmux.DirDown)
		case "tab":
			m.cycleMode()
		case "a":
			m.enableAll()
		case "n":
			m.disableAll()
		case "r":
			m.pollPanes()
			return m, fetchLayoutCmd(m.ownWindowID)
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case initMsg:
		if msg.err != nil {
			m.err = msg.err
			m.errTime = time.Now()
			return m, nil
		}
		m.ownPaneID = msg.ownPaneID
		m.ownWindowID = msg.ownWindowID
		m.updateLayout(msg.layout)
		m.pollPanes() // Poll immediately
		return m, tickCmd()

	case layoutUpdateMsg:
		if msg.err != nil {
			m.err = msg.err
			m.errTime = time.Now()
		} else {
			m.updateLayout(msg.layout)
		}

	case pollTickMsg:
		// Clear errors after 10 seconds
		if m.err != nil && time.Since(m.errTime) > 10*time.Second {
			m.err = nil
		}
		m.pollPanes()
		return m, tea.Batch(fetchLayoutCmd(m.ownWindowID), tickCmd())
	}

	return m, nil
}

func (m *Model) pollPanes() {
	if m.layout == nil {
		return
	}

	for _, pane := range m.layout.Panes {
		// Skip our own pane
		if pane.ID == m.ownPaneID {
			pane.HasClaudeCode = false
			continue
		}

		content, err := tmux.CapturePane(pane.ID)
		if err != nil {
			continue
		}

		pane.HasClaudeCode = detection.IsClaudeCode(content)

		// Check rate limit status for Claude Code panes
		if pane.HasClaudeCode {
			applyRateLimit(pane, detection.CheckRateLimit(content))

			// Auto-continue logic for rate-limited panes in auto mode
			if pane.IsRateLimited && pane.Mode == tmux.ModeContinueOnRateLimit {
				now := time.Now()

				if !pane.RateLimitTime.IsZero() {
					// Known reset time: send continue when time has passed
					if !pane.ContinueSent && now.After(pane.RateLimitTime) {
						m.sendContinue(pane.ID)
						pane.ContinueSent = true
					}
				} else {
					// Unknown reset time: send continue every 15 minutes
					periodicInterval := 15 * time.Minute
					if pane.LastPeriodicContinue.IsZero() || now.Sub(pane.LastPeriodicContinue) >= periodicInterval {
						m.sendContinue(pane.ID)
						pane.LastPeriodicContinue = now
					}
				}
			}

			// Test mode: trigger on test pattern (with cooldown)
			if m.testPattern != "" &&
				strings.Contains(content, m.testPattern) &&
				pane.Mode == tmux.ModeContinueOnRateLimit &&
				!pane.ContinueSent {
				m.sendContinue(pane.ID)
				pane.ContinueSent = true
			}
		} else {
			pane.ClearRateLimit()
		}
	}
}

// applyRateLimit folds a fresh reading into a pane's rate limit state.
//
// The deadline is pinned when a limit first appears and re-pinned only when
// the message itself changes. A relative reset like "8m" counts from the
// moment it was read, so recomputing it on every poll would push the deadline
// another three seconds out forever and no continue would ever be sent. The
// same check re-arms us for a second limit later in the session, which would
// otherwise look like the first one still sitting in the scrollback.
func applyRateLimit(pane *tmux.Pane, status detection.RateLimitStatus) {
	switch {
	case !status.IsLimited:
		pane.ClearRateLimit()
	case !pane.IsRateLimited || status.ResetsAt != pane.RateLimitResets:
		pane.IsRateLimited = true
		pane.RateLimitResets = status.ResetsAt
		pane.RateLimitTime = status.ResetTime
		pane.ContinueSent = false
		pane.LastPeriodicContinue = time.Time{}
	}
}

// sendContinue sends the continue command sequence to a pane
func (m *Model) sendContinue(paneID string) {
	// Send: Escape (dismiss any menu), wait for UI to settle, then "continue", Enter
	_ = tmux.SendKeys(paneID, "Escape")
	time.Sleep(100 * time.Millisecond) // Wait for Escape to be processed
	_ = tmux.SendKeys(paneID, "continue")
	_ = tmux.SendKeys(paneID, "Enter")

	// Track for UI feedback
	m.lastContinueSent = time.Now()
	m.lastContinuePane = paneID
}

func (m *Model) updateLayout(layout *tmux.Layout) {
	// Preserve state from old layout
	if m.layout != nil && layout != nil {
		for _, newPane := range layout.Panes {
			if oldPane := m.layout.PaneByID(newPane.ID); oldPane != nil {
				newPane.Mode = oldPane.Mode
				newPane.HasClaudeCode = oldPane.HasClaudeCode
				newPane.IsRateLimited = oldPane.IsRateLimited
				newPane.RateLimitResets = oldPane.RateLimitResets
				newPane.RateLimitTime = oldPane.RateLimitTime
				newPane.ContinueSent = oldPane.ContinueSent
				newPane.LastPeriodicContinue = oldPane.LastPeriodicContinue
			}
		}
	}

	m.layout = layout

	// Ensure we have a selected pane
	if layout != nil && len(layout.Panes) > 0 {
		// Keep current selection if still valid
		if m.selectedPaneID != "" && layout.PaneByID(m.selectedPaneID) != nil {
			return
		}
		// Otherwise select first pane
		m.selectedPaneID = layout.Panes[0].ID
	}
}

func (m *Model) moveSelection(dir tmux.Direction) {
	if m.layout == nil {
		return
	}

	current := m.layout.PaneByID(m.selectedPaneID)
	if current == nil {
		return
	}

	next := m.layout.PaneInDirection(current, dir)
	if next != nil {
		m.selectedPaneID = next.ID
	}
}

func (m *Model) cycleMode() {
	if m.layout == nil {
		return
	}

	pane := m.layout.PaneByID(m.selectedPaneID)
	if pane == nil {
		return
	}

	// Only allow mode changes on Claude Code panes
	if !pane.HasClaudeCode {
		return
	}

	if pane.Mode == tmux.ModeOff {
		pane.Mode = tmux.ModeContinueOnRateLimit
		// Check rate limit immediately when enabling
		m.checkPaneRateLimit(pane)
	} else {
		pane.Mode = tmux.ModeOff
	}
}

// checkPaneRateLimit checks the rate limit status for a single pane
func (m *Model) checkPaneRateLimit(pane *tmux.Pane) {
	if pane == nil || pane.ID == m.ownPaneID {
		return
	}

	content, err := tmux.CapturePane(pane.ID)
	if err != nil {
		return
	}

	applyRateLimit(pane, detection.CheckRateLimit(content))
	// Re-arm: the user just asked us to watch this pane, so send a continue
	// even if we already sent one for this limit. Both counters have to go:
	// the known-reset-time path gates on ContinueSent, the periodic path for
	// unknown reset times gates on LastPeriodicContinue.
	if pane.IsRateLimited {
		pane.ContinueSent = false
		pane.LastPeriodicContinue = time.Time{}
	}
}

func (m *Model) enableAll() {
	if m.layout == nil {
		return
	}
	for _, pane := range m.layout.Panes {
		if pane.HasClaudeCode {
			pane.Mode = tmux.ModeContinueOnRateLimit
			m.checkPaneRateLimit(pane)
		}
	}
}

func (m *Model) disableAll() {
	if m.layout == nil {
		return
	}
	for _, pane := range m.layout.Panes {
		if pane.HasClaudeCode {
			pane.Mode = tmux.ModeOff
		}
	}
}

func (m Model) View() string {
	// Show help overlay if active
	if m.showHelp {
		return m.renderHelp()
	}

	// Header with title and version
	title := titleStyle.Render("autoclaude")
	version := versionStyle.Render(fmt.Sprintf("v%s", m.version))
	headerWidth := m.width - 4
	if headerWidth < 20 {
		headerWidth = 20
	}
	// Place title left, version right
	titleLen := lipgloss.Width(title)
	versionLen := lipgloss.Width(version)
	spacerLen := headerWidth - titleLen - versionLen
	if spacerLen < 1 {
		spacerLen = 1
	}
	spacer := lipgloss.NewStyle().Width(spacerLen).Render("")
	header := headerStyle.Render(title + spacer + version)

	// Calculate main pane dimensions
	mainWidth := m.width - 4
	if mainWidth < 10 {
		mainWidth = 10
	}
	mainHeight := m.height - 7 // Account for header + 2-line footer + margins
	if mainHeight < 3 {
		mainHeight = 3
	}

	// Render content
	var content string
	if m.err != nil {
		content = errorStyle.Render(fmt.Sprintf("Error: %v", m.err))
	} else if m.layout == nil || len(m.layout.Panes) == 0 {
		content = dimTextStyle.Render("No panes found")
	} else {
		// Render the ASCII layout
		layoutWidth := mainWidth - 4 // Account for padding
		layoutHeight := mainHeight - 2
		content = renderLayout(m.layout, m.selectedPaneID, layoutWidth, layoutHeight)
	}

	mainPane := mainPaneStyle.
		Width(mainWidth).
		Height(mainHeight).
		Render(content)

	// Footer with selected pane status (left) and help (right)
	var statusText string

	// Show "continue sent" message for 20 seconds after sending
	if !m.lastContinueSent.IsZero() && time.Since(m.lastContinueSent) < 20*time.Second {
		statusText = lipgloss.NewStyle().Foreground(lipgloss.Color("#f1fa8c")).Bold(true).Render("↳ continue sent!")
	} else if m.layout != nil {
		if pane := m.layout.PaneByID(m.selectedPaneID); pane != nil {
			if pane.HasClaudeCode {
				// Always show auto-continue status first
				if pane.Mode == tmux.ModeContinueOnRateLimit {
					statusText = lipgloss.NewStyle().Foreground(lipgloss.Color("#50fa7b")).Render("● Auto-continue enabled")
					// Add rate limit info on same line if applicable (only when auto mode)
					if pane.IsRateLimited {
						if pane.ContinueSent {
							statusText += lipgloss.NewStyle().Foreground(lipgloss.Color("#f1fa8c")).Bold(true).Render(" continue sent")
						} else if pane.RateLimitResets != "" {
							statusText += errorStyle.Render(" resets " + pane.RateLimitResets)
						} else {
							statusText += errorStyle.Render(" rate limited")
						}
					}
				} else {
					statusText = dimTextStyle.Render("○ Auto-continue disabled")
				}
			}
		}
	}

	helpText := dimTextStyle.Render("←↑↓→ nav • tab toggle • a on • n off • r refresh • h help • q quit")

	// Footer: status on first line, help on second line (both left-aligned)
	var footer string
	if statusText != "" {
		footer = "  " + statusText + "\n  " + helpText
	} else {
		footer = "  " + helpText
	}

	// Compose the full view
	return lipgloss.JoinVertical(lipgloss.Left, header, mainPane, footer)
}

func (m Model) renderHelp() string {
	helpStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accentCyan).
		Padding(1, 2).
		Width(m.width - 4)

	titleLine := titleStyle.Render("autoclaude") + " " + versionStyle.Render(fmt.Sprintf("v%s", m.version))

	helpContent := `
Monitors tmux panes running Claude Code and automatically
sends "continue" when rate limits reset.

` + lipgloss.NewStyle().Bold(true).Foreground(accentCyan).Render("KEYS") + `

  ←↑↓→      Navigate between panes
  tab       Toggle auto-continue for selected pane
  a         Enable auto-continue for all Claude Code panes
  n         Disable auto-continue for all Claude Code panes
  r         Refresh pane layout
  h / ?     Show this help
  q         Quit

` + lipgloss.NewStyle().Bold(true).Foreground(accentCyan).Render("PANE COLORS") + `

  ` + lipgloss.NewStyle().Foreground(claudeOrange).Render("Orange") + `      Claude Code pane (auto-continue off)
  ` + lipgloss.NewStyle().Foreground(autoGreen).Render("Green") + `       Claude Code pane (auto-continue on)
  ` + lipgloss.NewStyle().Foreground(rateLimitRed).Render("Red") + `         Rate limited (waiting for reset)
  ` + lipgloss.NewStyle().Foreground(accentCyan).Render("Cyan") + `        Selected pane

` + lipgloss.NewStyle().Bold(true).Foreground(accentCyan).Render("HOW IT WORKS") + `

  When a Claude Code pane shows a rate limit message like
  "limit reached ∙ resets Xpm" or "You've hit your limit",
  autoclaude waits for that time to pass, then sends:
  Escape → "continue" → Enter

  Polling occurs every 3 seconds.

` + dimTextStyle.Render("Made by Henry Stanley (henrystanley.com)") + `
` + dimTextStyle.Render("Built with Claude Code")

	footer := dimTextStyle.Render("Press any key to close")

	return lipgloss.JoinVertical(lipgloss.Left,
		"",
		helpStyle.Render(titleLine+helpContent),
		"  "+footer,
	)
}
