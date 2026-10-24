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
	"github.com/floatpane/matcha/internal/headerindex"
	"github.com/floatpane/matcha/theme"
)

// searchResultRows is how many results the overlay shows at once; the
// window scrolls to keep the highlighted result visible.
const searchResultRows = 10

// localResultLimit is the most matches shown from the copy on this computer.
const localResultLimit = 200

// LocalSearcher searches the email headers copied to this computer.
type LocalSearcher interface {
	// CanSearch reports whether the query only uses fields the copy has.
	CanSearch(query backend.SearchQuery) bool
	Search(query backend.SearchQuery, folder string, accountIDs []string, limit int) []fetcher.Email
	// Count is how many emails of the folder are copied for the accounts.
	Count(folder string, accountIDs []string) int
	// Syncing reports whether the copy of the folder is being updated.
	Syncing(folder string, accountIDs []string) bool
}

// HiddenSearcher reads hidden headers and recipient counts from the local copy.
type HiddenSearcher interface {
	HiddenCount(folder string, accountIDs []string) int
	Hidden(folder string, accountIDs []string) []fetcher.Email
	HiddenRecipients(accountID, folder string) []headerindex.AddressCount
}

// SearchHistory saves the queries the user searched for, newest first.
type SearchHistory interface {
	Searches() ([]string, error)
	// Add saves query as the newest search and returns the saved searches.
	Add(query string) ([]string, error)
	// Remove deletes query and returns the saved searches.
	Remove(query string) ([]string, error)
	Clear() error
}

type SearchOverlay struct {
	input   textinput.Model
	query   backend.SearchQuery
	results []fetcher.Email
	loading bool
	done    bool
	width   int

	// local searches the header copy while typing; nil when there is none.
	// fromServer tells whether results came from the server (Enter) or the copy.
	local      LocalSearcher
	folder     string
	accountIDs []string
	fromServer bool

	// history saves searches; nil when there is none. recent holds the saved
	// searches, listed under the query while it is empty.
	history SearchHistory
	recent  []string

	// cursor is the highlighted row of the list under the query (results, or
	// recent searches); firstShown is the first row in the visible window.
	// resultsFocused means keys move the highlight instead of editing the query.
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

// UseLocalSearch makes typing search the header copy of folder for the accounts.
func (o *SearchOverlay) UseLocalSearch(local LocalSearcher, folder string, accountIDs []string) {
	o.local, o.folder, o.accountIDs = local, folder, accountIDs
}

// UseSearchHistory lists the saved searches under the empty query and saves
// new ones to history.
func (o *SearchOverlay) UseSearchHistory(history SearchHistory) tea.Cmd {
	o.history = history
	recent, err := history.Searches()
	if err != nil {
		return notifyError(err)
	}
	o.recent = recent
	return nil
}

func notifyError(err error) tea.Cmd {
	return func() tea.Msg { return NotifyMsg{Message: err.Error()} }
}

func (o *SearchOverlay) Init() tea.Cmd { return textinput.Blink }

func (o *SearchOverlay) Update(msg tea.Msg, mailbox MailboxKind, accountID string) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		o.width = msg.Width
		return nil
	case SearchResultsMsg:
		if !o.loading || msg.Query.Raw != o.input.Value() {
			// The query was edited while the server searched; these results are stale.
			return nil
		}
		o.loading, o.done = false, msg.Err == nil
		o.query = msg.Query
		if msg.Err != nil {
			return notifyError(msg.Err)
		}
		o.results = msg.Emails
		o.fromServer = true
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
			search := func() tea.Msg { return SearchRequestedMsg{Query: query, Mailbox: mailbox, AccountID: accountID} }
			return tea.Batch(o.remember(raw), search)
		}
		if o.resultsFocused {
			// Typing while the results are focused goes back to editing the query.
			o.focusInput()
		}
	}

	before := o.input.Value()
	var cmd tea.Cmd
	o.input, cmd = o.input.Update(msg)
	if after := o.input.Value(); after != before {
		o.loading = false
		if before == "" || after == "" {
			// The list under the query switches between recent searches and results.
			o.cursor, o.firstShown = 0, 0
		}
		o.searchLocally()
	}
	return cmd
}

// remember saves query as the newest recent search.
func (o *SearchOverlay) remember(query string) tea.Cmd {
	if o.history == nil {
		return nil
	}
	recent, err := o.history.Add(query)
	if err != nil {
		return notifyError(err)
	}
	o.recent = recent
	return nil
}

// searchLocally replaces the results with matches from the header copy.
// Without a copy, earlier server results stay until the next Enter.
func (o *SearchOverlay) searchLocally() {
	if o.local == nil {
		return
	}
	o.fromServer = false
	o.cursor, o.firstShown = 0, 0
	o.results, o.done = nil, false
	o.query = backend.ParseSearchQuery(o.input.Value())
	if !o.local.CanSearch(o.query) {
		return
	}
	o.results = o.local.Search(o.query, o.folder, o.accountIDs, localResultLimit)
	o.done = true
}

// RefreshLocal reruns the local search after the header copy changed, keeping
// the highlight on the same row where possible. Server results stay as they are.
func (o *SearchOverlay) RefreshLocal() {
	if o.local == nil || o.fromServer || o.loading || !o.done {
		return
	}
	cursor, firstShown := o.cursor, o.firstShown
	o.results = o.local.Search(o.query, o.folder, o.accountIDs, localResultLimit)
	if len(o.results) == 0 {
		o.cursor, o.firstShown = 0, 0
		return
	}
	o.cursor = min(cursor, len(o.results)-1)
	o.firstShown = min(firstShown, o.cursor)
}

// showingRecent reports whether the list under the query is the recent
// searches, which happens while the query is empty.
func (o *SearchOverlay) showingRecent() bool {
	return o.input.Value() == "" && len(o.recent) > 0
}

// rowCount is how many rows the list under the query has.
func (o *SearchOverlay) rowCount() int {
	switch {
	case o.showingRecent():
		return len(o.recent)
	case o.done:
		return len(o.results)
	}
	return 0
}

// handleResultKey moves the highlight, opens the highlighted result or uses,
// removes and clears recent searches. It reports false for keys that belong
// to the query input.
func (o *SearchOverlay) handleResultKey(key string) (tea.Cmd, bool) {
	if o.rowCount() == 0 {
		return nil, false
	}
	kb := config.Keybinds.Global
	switch key {
	case keyDown, "ctrl+n":
		if !o.resultsFocused {
			// The first ↓ from the query lands on the highlighted first result.
			o.focusResults()
			return nil, true
		}
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
		return o.pickHighlighted(), true
	}
	if o.showingRecent() {
		switch key {
		case "d", "delete":
			return o.removeRecent(), true
		case "D":
			return o.clearRecent(), true
		}
	}
	return nil, false
}

// MoveHighlight moves the highlighted result by delta, e.g. for the mouse wheel.
func (o *SearchOverlay) MoveHighlight(delta int) {
	if o.rowCount() > 0 {
		o.focusResults()
		o.moveCursor(delta)
	}
}

// Click handles a left click on row y of the overlay: a click highlights a
// result, a second click on the same result within 500 ms picks it.
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
		return o.pickHighlighted()
	}
	return nil
}

// pickHighlighted opens the highlighted result, or uses the highlighted recent search.
func (o *SearchOverlay) pickHighlighted() tea.Cmd {
	if o.showingRecent() {
		return o.useRecent()
	}
	return o.openHighlighted()
}

func (o *SearchOverlay) openHighlighted() tea.Cmd {
	var remembered tea.Cmd
	if !o.fromServer {
		// Enter saved the server searches already; save a search answered by the copy here.
		remembered = o.remember(o.input.Value())
	}
	results := append([]fetcher.Email(nil), o.results...)
	open := results[o.cursor]
	query := o.query
	apply := func() tea.Msg { return ApplySearchResultsMsg{Query: query, Emails: results, Open: &open} }
	return tea.Batch(remembered, apply)
}

// useRecent puts the highlighted recent search in the query and searches the
// copy on this computer; a second Enter searches the server.
func (o *SearchOverlay) useRecent() tea.Cmd {
	o.input.SetValue(o.recent[o.cursor])
	o.input.CursorEnd()
	o.cursor, o.firstShown = 0, 0
	o.loading = false
	o.focusInput()
	o.searchLocally()
	return textinput.Blink
}

func (o *SearchOverlay) removeRecent() tea.Cmd {
	recent, err := o.history.Remove(o.recent[o.cursor])
	if err != nil {
		return notifyError(err)
	}
	o.recent = recent
	return o.keepHighlightInRecent()
}

func (o *SearchOverlay) clearRecent() tea.Cmd {
	if err := o.history.Clear(); err != nil {
		return notifyError(err)
	}
	o.recent = nil
	return o.keepHighlightInRecent()
}

// keepHighlightInRecent moves the highlight onto a remaining recent search,
// or back to the query when none are left.
func (o *SearchOverlay) keepHighlightInRecent() tea.Cmd {
	if len(o.recent) == 0 {
		o.cursor, o.firstShown = 0, 0
		o.focusInput()
		return textinput.Blink
	}
	o.cursor = min(o.cursor, len(o.recent)-1)
	o.firstShown = max(0, min(o.firstShown, o.cursor, len(o.recent)-searchResultRows))
	return nil
}

func (o *SearchOverlay) moveCursor(delta int) {
	count := o.rowCount()
	o.cursor = (o.cursor + delta + count) % count
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

// statusLines says where the results come from and which keys do what.
func (o *SearchOverlay) statusLines() []string {
	pick := "↓ or Tab to pick a result, Enter to search the server (also bodies), Esc to close."
	if o.resultsFocused {
		pick = "j/k or ↑/↓ to pick, Enter to open, Tab to edit search, Esc to close."
	}
	switch {
	case o.loading:
		return []string{"Searching the server..."}
	case o.showingRecent():
		return o.recentStatusLines()
	case o.fromServer && o.done:
		lines := []string{fmt.Sprintf("%d result(s) from the server.", len(o.results))}
		if len(o.results) > 0 {
			lines = append(lines, pick)
		}
		return lines
	case o.local == nil:
		if o.input.Value() == "" {
			return nil
		}
		return []string{"Enter to search the server, Esc to close."}
	}
	copied := o.copiedText()
	switch {
	case o.input.Value() == "":
		return []string{
			"Type to search senders, recipients and subjects (" + copied + ").",
			"Enter searches the server, also in email bodies. Esc to close.",
		}
	case !o.done:
		return []string{"body: and larger: are only searched on the server: press Enter."}
	case len(o.results) == 0:
		return []string{
			"No matches on this computer (" + copied + ").",
			"Enter searches the server, also in email bodies. Esc to close.",
		}
	}
	return []string{fmt.Sprintf("%d match(es) on this computer (%s).", len(o.results), copied), pick}
}

// recentStatusLines tells the keys for the recent searches list.
func (o *SearchOverlay) recentStatusLines() []string {
	if o.resultsFocused {
		return []string{
			"j/k or ↑/↓ to pick, Enter to use the search, Tab to type, Esc to close.",
			"d or Delete removes the highlighted search, D clears all recent searches.",
		}
	}
	typing := "Type a search, then Enter searches the server."
	if o.local != nil {
		typing = "Type to search senders, recipients and subjects (" + o.copiedText() + ")."
	}
	return []string{typing, "↓ or Tab to pick a recent search. Esc to close."}
}

// copiedText says how much of the folder is copied to this computer.
func (o *SearchOverlay) copiedText() string {
	copied := fmt.Sprintf("%d emails of %s copied to this computer", o.local.Count(o.folder, o.accountIDs), o.folder)
	if o.local.Syncing(o.folder, o.accountIDs) {
		copied += ", updating"
	}
	return copied
}

// textAboveResults is the overlay text drawn before the first result row.
// Every line fits the box, so the row count matches what is drawn.
func (o *SearchOverlay) textAboveResults() string {
	textWidth := o.textWidth()
	o.input.SetWidth(max(1, textWidth-lipgloss.Width(o.input.Prompt)-1))
	content := "Search mail\n\n" + o.input.View()
	if lines := o.statusLines(); len(lines) > 0 {
		content += "\n"
		for _, line := range lines {
			content += "\n" + ansi.Truncate(line, textWidth, "…")
		}
		switch {
		case o.showingRecent():
			content += "\n\nRecent searches\n"
		case o.done:
			content += "\n"
		}
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
	switch {
	case o.showingRecent():
		content += o.rowsView(func(i int) string { return o.recent[i] })
	case o.done:
		content += o.rowsView(func(i int) string {
			email := o.results[i]
			return fmt.Sprintf("%d. %s - %s", i+1, parseSenderName(email.From), email.Subject)
		})
	}
	return o.boxStyle().Render(content)
}

// resultAt returns the row of the list under the query drawn on row y of
// View, or -1.
func (o *SearchOverlay) resultAt(y int) int {
	count := o.rowCount()
	if count == 0 {
		return -1
	}
	style := o.boxStyle()
	row := y - style.GetBorderTopSize() - style.GetPaddingTop() - strings.Count(o.textAboveResults(), "\n")
	shown := min(searchResultRows, count-o.firstShown)
	if row < 0 || row >= shown {
		return -1
	}
	return o.firstShown + row
}

// rowsView draws the visible window of the list under the query, one line
// per row with label giving the text of row i.
func (o *SearchOverlay) rowsView(label func(i int) string) string {
	textWidth := o.textWidth()
	highlight := lipgloss.NewStyle().Foreground(theme.ActiveTheme.Accent).Bold(true)
	last := min(o.firstShown+searchResultRows, o.rowCount())
	var b strings.Builder
	for i := o.firstShown; i < last; i++ {
		marker := "  "
		if i == o.cursor {
			marker = "> "
		}
		// One line per row keeps clicks mapped to the right row.
		line := ansi.Truncate(marker+label(i), textWidth, "…")
		if i == o.cursor && o.resultsFocused {
			line = highlight.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}
