package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/floatpane/matcha/backend"
	"github.com/floatpane/matcha/config"
	"github.com/floatpane/matcha/fetcher"
	"github.com/floatpane/matcha/theme"
)

// searchResultRows is how many results the overlay shows at once; the
// window scrolls to keep the highlighted result visible.
const searchResultRows = 10

type SearchOverlay struct {
	input   textinput.Model
	query   backend.SearchQuery
	results []fetcher.Email
	loading bool
	done    bool
	width   int

	// cursor is the highlighted result; firstShown is the first result in
	// the visible window. resultsFocused means keys move the highlight
	// instead of editing the query.
	cursor         int
	firstShown     int
	resultsFocused bool
	lastClickTime  time.Time
	lastClickRow   int
}

func NewSearchOverlay(width, height int) *SearchOverlay {
	ti := textinput.New()
	ti.Placeholder = "from:alice subject:invoice since:2026-01-01"
	ti.Prompt = "/ "
	ti.CharLimit = 256
	ti.Focus()
	ti.SetStyles(ThemedTextInputStyles())
	if width < 44 {
		width = 44
	}
	return &SearchOverlay{input: ti, width: width}
}

func (o *SearchOverlay) Init() tea.Cmd { return textinput.Blink }

func (o *SearchOverlay) Update(msg tea.Msg, mailbox MailboxKind, accountID string) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		o.width = msg.Width
		return nil
	case SearchResultsMsg:
		o.loading, o.done = false, msg.Err == nil
		o.query = msg.Query
		if msg.Err != nil {
			return func() tea.Msg { return NotifyMsg{Message: msg.Err.Error()} }
		}
		o.results = msg.Emails
		o.cursor, o.firstShown = 0, 0
		if len(o.results) > 0 {
			o.focusResults()
		}
		return nil
	case tea.KeyPressMsg:
		if cmd, handled := o.handleResultKey(msg.String()); handled {
			return cmd
		}
		if msg.String() == keyEnter {
			if o.loading {
				return nil
			}
			raw := o.input.Value()
			if raw == "" {
				return nil
			}
			o.loading, o.done, o.results = true, false, nil
			query := backend.ParseSearchQuery(raw)
			return func() tea.Msg { return SearchRequestedMsg{Query: query, Mailbox: mailbox, AccountID: accountID} }
		}
		if o.resultsFocused {
			// Typing while the results are focused goes back to editing the query.
			o.focusInput()
		}
	}

	var cmd tea.Cmd
	o.input, cmd = o.input.Update(msg)
	return cmd
}

// handleResultKey moves the highlight or opens the highlighted result.
// It reports false for keys that belong to the query input.
func (o *SearchOverlay) handleResultKey(key string) (tea.Cmd, bool) {
	if !o.done || len(o.results) == 0 {
		return nil, false
	}
	kb := config.Keybinds.Global
	switch key {
	case keyDown, "ctrl+n":
		o.focusResults()
		o.moveCursor(1)
		return nil, true
	case "up", "ctrl+p":
		o.focusResults()
		o.moveCursor(-1)
		return nil, true
	case "tab", "shift+tab":
		if o.resultsFocused {
			o.focusInput()
			return textinput.Blink, true
		}
		o.focusResults()
		return nil, true
	}
	if !o.resultsFocused {
		return nil, false
	}
	switch key {
	case kb.NavDown:
		o.moveCursor(1)
		return nil, true
	case kb.NavUp:
		o.moveCursor(-1)
		return nil, true
	case keyEnter:
		return o.openHighlighted(), true
	}
	return nil, false
}

// MoveHighlight moves the highlighted result by delta, e.g. for the mouse wheel.
func (o *SearchOverlay) MoveHighlight(delta int) {
	if o.done && len(o.results) > 0 {
		o.focusResults()
		o.moveCursor(delta)
	}
}

// Click handles a left click on row y of the overlay: a click highlights a
// result, a second click on the same result within 500 ms opens it.
func (o *SearchOverlay) Click(y int) tea.Cmd {
	index := o.resultAt(y)
	if index < 0 {
		return nil
	}
	now := time.Now()
	doubleClick := index == o.lastClickRow && now.Sub(o.lastClickTime) < 500*time.Millisecond
	o.lastClickTime, o.lastClickRow = now, index
	o.focusResults()
	o.cursor = index
	if doubleClick {
		return o.openHighlighted()
	}
	return nil
}

func (o *SearchOverlay) openHighlighted() tea.Cmd {
	results := append([]fetcher.Email(nil), o.results...)
	open := results[o.cursor]
	query := o.query
	return func() tea.Msg { return ApplySearchResultsMsg{Query: query, Emails: results, Open: &open} }
}

func (o *SearchOverlay) moveCursor(delta int) {
	o.cursor = (o.cursor + delta + len(o.results)) % len(o.results)
	if o.cursor < o.firstShown {
		o.firstShown = o.cursor
	} else if o.cursor >= o.firstShown+searchResultRows {
		o.firstShown = o.cursor - searchResultRows + 1
	}
}

func (o *SearchOverlay) focusResults() {
	o.resultsFocused = true
	o.input.Blur()
}

func (o *SearchOverlay) focusInput() {
	o.resultsFocused = false
	o.input.Focus()
}

func (o *SearchOverlay) boxStyle() lipgloss.Style {
	boxWidth := o.width - 4
	if boxWidth < 40 {
		boxWidth = 40
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.ActiveTheme.Accent).Padding(1, 2).Width(boxWidth)
}

// textAboveResults is the overlay text drawn before the first result row.
// Every line fits the box, so the row count matches what is drawn.
func (o *SearchOverlay) textAboveResults() string {
	textWidth := o.textWidth()
	o.input.SetWidth(max(1, textWidth-lipgloss.Width(o.input.Prompt)-1))
	content := "Search mail\n\n" + o.input.View()
	if o.loading {
		content += "\n\nSearching..."
	}
	if o.done {
		help := "Esc to close."
		switch {
		case o.resultsFocused:
			help = "j/k or ↑/↓ to pick, Enter to open, Tab to edit search, Esc to close."
		case len(o.results) > 0:
			help = "↓ or Tab to pick a result, Esc to close."
		}
		status := fmt.Sprintf("%d result(s). %s", len(o.results), help)
		content += "\n\n" + ansi.Truncate(status, textWidth, "…") + "\n"
	}
	return content
}

// textWidth is the width available for text inside the box.
func (o *SearchOverlay) textWidth() int {
	style := o.boxStyle()
	return style.GetWidth() - style.GetHorizontalFrameSize()
}

func (o *SearchOverlay) View() string {
	content := o.textAboveResults()
	if o.done {
		content += o.resultsView()
	}
	return o.boxStyle().Render(content)
}

// resultAt returns the result drawn on row y of View, or -1.
func (o *SearchOverlay) resultAt(y int) int {
	if !o.done {
		return -1
	}
	style := o.boxStyle()
	row := y - style.GetBorderTopSize() - style.GetPaddingTop() - strings.Count(o.textAboveResults(), "\n")
	shown := min(searchResultRows, len(o.results)-o.firstShown)
	if row < 0 || row >= shown {
		return -1
	}
	return o.firstShown + row
}

func (o *SearchOverlay) resultsView() string {
	textWidth := o.textWidth()
	highlight := lipgloss.NewStyle().Foreground(theme.ActiveTheme.Accent).Bold(true)
	last := min(o.firstShown+searchResultRows, len(o.results))
	var b strings.Builder
	for i := o.firstShown; i < last; i++ {
		email := o.results[i]
		marker := "  "
		if i == o.cursor {
			marker = "> "
		}
		// One row per result keeps clicks mapped to the right email.
		line := ansi.Truncate(fmt.Sprintf("%s%d. %s - %s", marker, i+1, parseSenderName(email.From), email.Subject), textWidth, "…")
		if i == o.cursor && o.resultsFocused {
			line = highlight.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}
