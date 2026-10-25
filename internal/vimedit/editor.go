// Package vimedit adds optional Vim-style editing to an existing Bubbles
// textarea. The textarea remains the sole editable text buffer.
package vimedit

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

// Mode is the current editing mode.
type Mode uint8

const (
	Normal Mode = iota
	Insert
	Visual
	VisualLine
)

func (m Mode) String() string {
	switch m {
	case Insert:
		return "INSERT"
	case Visual:
		return "VISUAL"
	case VisualLine:
		return "VISUAL LINE"
	default:
		return "NORMAL"
	}
}

type snapshot struct {
	text   string
	cursor position
}

type register struct {
	text     string
	linewise bool
}

// Editor owns modal state, an internal register, and undo history. It must not
// be shared between unrelated textareas; call Reset when loading another body.
type Editor struct {
	mode        Mode
	count       int
	op          string
	opCount     int
	opExplicit  bool
	prefix      string
	anchor      position
	register    register
	past        []snapshot
	future      []snapshot
	transaction *snapshot
	sequence    []tea.Msg
	lastEdit    []tea.Msg
	replaying   bool
	insertCount int
	insertStart int
	insertOpen  bool
	command     [16]tea.Msg
}

func New() *Editor {
	e := &Editor{}
	e.sequence = e.command[:0]
	return e
}

func (e *Editor) Mode() Mode { return e.mode }

// Reset forgets all editing state without touching the supplied widget.
func (e *Editor) Reset() {
	*e = Editor{}
	e.sequence = e.command[:0]
}

// Leave cancels partial commands and selections, and commits an insert session
// as one undo step. It never changes focus or exits the surrounding composer.
func (e *Editor) Leave(input *textarea.Model) {
	if e.mode == Insert {
		e.sequence = append(e.sequence, tea.KeyPressMsg{Code: tea.KeyEscape})
		e.finishInsert(input)
	} else {
		input.ClearSelection()
		e.mode = Normal
		e.clearPending()
	}
}

func takeSnapshot(input *textarea.Model) snapshot {
	return snapshot{text: input.Value(), cursor: cursor(input)}
}

func (e *Editor) begin(input *textarea.Model) {
	if e.transaction == nil {
		s := takeSnapshot(input)
		e.transaction = &s
	}
}

func (e *Editor) finish(input *textarea.Model) {
	if e.transaction != nil {
		if e.transaction.text != input.Value() {
			e.past = append(e.past, *e.transaction)
			e.future = nil
			if !e.replaying {
				e.lastEdit = append([]tea.Msg(nil), e.sequence...)
			}
		}
		e.transaction = nil
	}
	e.clearPending()
}

func (e *Editor) clearPending() {
	e.count, e.opCount = 0, 0
	e.op, e.prefix = "", ""
	e.opExplicit = false
	clear(e.sequence)
	e.sequence = e.sequence[:0]
}

func restore(input *textarea.Model, s snapshot) {
	input.SetValue(s.text)
	moveCursor(input, s.cursor)
	input.ClearSelection()
}

func (e *Editor) undo(input *textarea.Model, redo bool, count int) {
	from, to := &e.past, &e.future
	if redo {
		from, to = to, from
	}
	for range min(count, len(*from)) {
		*to = append(*to, takeSnapshot(input))
		s := (*from)[len(*from)-1]
		*from = (*from)[:len(*from)-1]
		restore(input, s)
	}
	normalCursor(input)
	e.clearPending()
}

func (e *Editor) enterInsert(input *textarea.Model, count int) {
	e.begin(input)
	e.mode = Insert
	e.op, e.prefix = "", ""
	e.count, e.opCount = 0, 0
	e.opExplicit = false
	e.insertCount = count
	e.insertStart = len(e.sequence)
	e.insertOpen = false
	input.ClearSelection()
}

func (e *Editor) finishInsert(input *textarea.Model) {
	// A count on i/a/I/A repeats the literal insertion session, including paste
	// and editing keys. Operator counts instead determine the changed range.
	if e.insertCount > 1 {
		messages := e.sequence[e.insertStart : len(e.sequence)-1]
		limit := min(e.insertCount, max(1024, input.Length()+1))
		for i := 1; i < limit; i++ {
			if e.insertOpen {
				input.InsertString("\n")
			}
			for _, msg := range messages {
				updated, _ := input.Update(msg)
				*input = updated
			}
		}
	}
	e.insertCount = 0
	e.insertOpen = false
	e.mode = Normal
	input.ClearSelection()
	input.SetCursorColumn(max(0, input.Column()-1))
	normalCursor(input)
	e.finish(input)
}

// Update handles only body editing. Global composer shortcuts must be handled
// by the caller first. Returned commands are widget commands, never send/quit.
func (e *Editor) Update(input *textarea.Model, msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		name := key.String()
		if name == "esc" || name == "ctrl+[" {
			e.Leave(input)
			return nil
		}
		if e.mode == Insert {
			e.sequence = append(e.sequence, msg)
			updated, cmd := input.Update(msg)
			*input = updated
			return cmd
		}
		normalCursor(input)
		e.sequence = append(e.sequence, msg)
		e.key(input, name)
		// Refresh scrolling/blinking through the widget's ordinary event
		// path; never reconstruct the widget or replace text for a motion.
		updated, cmd := input.Update(nil)
		*input = updated
		return cmd
	}
	if paste, ok := msg.(tea.PasteMsg); ok {
		if e.mode == Insert {
			e.sequence = append(e.sequence, paste)
			input.InsertString(paste.Content)
			return nil
		}
		// Bracketed paste is literal even in Normal/Visual mode, not a stream
		// of commands. It is one undoable edit and can be dot-repeated.
		e.clearPending()
		e.sequence = append(e.sequence, paste)
		e.begin(input)
		if e.mode == Visual || e.mode == VisualLine {
			input.DeleteSelection()
		}
		input.ClearSelection()
		input.InsertString(paste.Content)
		e.mode = Normal
		input.SetCursorColumn(max(0, input.Column()-1))
		normalCursor(input)
		e.finish(input)
		return nil
	}
	updated, cmd := input.Update(msg)
	*input = updated
	return cmd
}

func (e *Editor) key(input *textarea.Model, key string) {
	if len(key) == 1 && key[0] >= '0' && key[0] <= '9' && (key != "0" || e.count != 0) && e.prefix == "" {
		// Keep counts below one billion to prevent arithmetic overflow.
		e.count = min(1_000_000_000, e.count*10+int(key[0]-'0'))
		return
	}
	explicit := e.count > 0 || e.opExplicit
	count := max(1, e.count)
	if e.op != "" {
		count = min(1_000_000_000, count*max(1, e.opCount))
	}
	if e.prefix == "g" {
		e.prefix = ""
		if key != "g" {
			e.cancel(input)
			return
		}
		key = "gg"
	} else if e.prefix == "i" {
		e.prefix = ""
		if e.op == "" || key != "w" && key != "W" {
			e.cancel(input)
			return
		}
		d := readDocument(input)
		start, end := d.innerWord(d.offset(cursor(input)), count, key == "W")
		e.operate(input, d.position(start), d.position(end), false)
		return
	}
	if key == "g" {
		e.prefix = "g"
		return
	}
	if e.mode == Visual || e.mode == VisualLine {
		e.visualKey(input, key, count, explicit)
		return
	}
	if e.op != "" {
		if key == "i" {
			e.prefix = "i"
			return
		}
		if key == e.op {
			e.lineOperation(input, input.Line(), input.Line()+min(count-1, input.LineCount()))
			return
		}
		if e.operatorMotion(input, key, count, explicit) {
			return
		}
		e.cancel(input)
		return
	}
	if motion(input, key, count, explicit) {
		e.clearPending()
		return
	}
	switch key {
	case "d", "c", "y":
		e.op, e.opCount, e.count = key, count, 0
		e.opExplicit = explicit
		return
	case "v", "V":
		e.mode = Visual
		if key == "V" {
			e.mode = VisualLine
		}
		e.anchor = cursor(input)
		e.count = 0
		e.updateVisual(input)
		return
	case "i", "I", "a", "A", "o", "O":
		e.begin(input)
		switch key {
		case "I":
			firstNonblank(input)
		case "a":
			input.SetCursorColumn(min(input.Column()+1, lineLength(input)))
		case "A":
			input.CursorEnd()
		case "o":
			input.CursorEnd()
			input.InsertString("\n")
		case "O":
			input.CursorStart()
			p := cursor(input)
			input.InsertString("\n")
			moveCursor(input, p)
		}
		e.enterInsert(input, count)
		e.insertOpen = key == "o" || key == "O"
		return
	case "x", "X", "s":
		p := cursor(input)
		start, end := p, p
		if key == "X" {
			start.Col = max(0, p.Col-count)
		} else {
			end.Col = min(lineLength(input), p.Col+count)
		}
		e.op = "d"
		if key == "s" {
			e.op = "c"
		}
		e.operate(input, start, end, false)
		return
	case "D", "C":
		e.op = "d"
		if key == "C" {
			e.op = "c"
		}
		e.operatorMotion(input, "$", count, true)
		return
	case "S":
		e.op = "c"
		e.lineOperation(input, input.Line(), input.Line()+min(count-1, input.LineCount()))
		return
	case "p", "P":
		e.begin(input)
		e.put(input, key == "P", count)
		e.finish(input)
		return
	case "u", "ctrl+r":
		e.undo(input, key == "ctrl+r", count)
		return
	case ".":
		messages := e.lastEdit
		e.clearPending()
		if len(messages) == 0 {
			return
		}
		e.replaying = true
		for range min(count, max(1024, input.Length()+1)) {
			for _, msg := range messages {
				e.Update(input, msg)
			}
		}
		e.replaying = false
		return
	}
	// Unsupported commands (including :, q, and Z) are inert, not actions.
	e.clearPending()
}

func (e *Editor) cancel(input *textarea.Model) {
	e.clearPending()
	if e.mode == Visual || e.mode == VisualLine {
		e.updateVisual(input)
	}
}

func (e *Editor) operatorMotion(input *textarea.Model, key string, count int, explicit bool) bool {
	p := cursor(input)
	if key == "j" || key == "down" || key == "k" || key == "up" || key == "gg" || key == "G" {
		if !motion(input, key, count, explicit) {
			return false
		}
		row := input.Line()
		moveCursor(input, p)
		e.lineOperation(input, min(p.Row, row), max(p.Row, row))
		return true
	}
	d := readDocument(input)
	start := d.offset(p)
	end := start
	switch key {
	case "w", "W", "b", "B", "e", "E":
		// cw/cW change to the word end, leaving following whitespace intact.
		if e.op == "c" && (key == "w" || key == "W") && start < len(d.runes) && wordClass(d.runes[start], key == "W") != 0 {
			_, end = d.innerWord(start, count, key == "W")
		} else {
			end = d.word(start, count, key)
			if key == "e" || key == "E" {
				end = min(len(d.runes), end+1)
			}
			// An exclusive w landing at column zero must not eat a newline
			// after the final selected word (Vim's operator exception).
			if (key == "w" || key == "W") && d.position(end).Row > p.Row && d.position(end).Col == 0 {
				end = max(start, end-1)
			}
		}
	case "$", "end":
		end = d.end(min(len(d.starts)-1, p.Row+min(count-1, len(d.starts))))
	case "0", "home":
		end = d.starts[p.Row]
	case "^":
		firstNonblank(input)
		end = d.offset(cursor(input))
		moveCursor(input, p)
	case "h", "left":
		end = max(d.starts[p.Row], start-count)
	case "l", "right":
		end = min(d.end(p.Row), start+count)
	default:
		return false
	}
	a, b := d.position(start), d.position(end)
	a, b = ordered(a, b)
	e.operate(input, a, b, false)
	return true
}

func (e *Editor) lineOperation(input *textarea.Model, first, last int) {
	d := readDocument(input)
	first = max(0, min(first, len(d.starts)-1))
	last = max(first, min(last, len(d.starts)-1))
	start, end := d.starts[first], d.end(last)
	text := string(d.runes[start:end]) + "\n"
	if last+1 < len(d.starts) {
		end++
	} else if first > 0 && e.op != "c" {
		start--
	}
	if e.op == "y" {
		e.register = register{text: text, linewise: true}
		e.mode = Normal
		input.ClearSelection()
		moveCursor(input, position{Row: first, Col: 0})
		e.clearPending()
		return
	}
	e.begin(input)
	selectRange(input, d.position(start), d.position(end))
	input.DeleteSelection()
	e.register = register{text: text, linewise: true}
	if e.op == "c" {
		// Preserve one empty logical line for the replacement text.
		if last+1 < len(d.starts) {
			p := cursor(input)
			input.InsertString("\n")
			moveCursor(input, p)
		}
		e.enterInsert(input, 1)
		return
	}
	e.mode = Normal
	moveCursor(input, position{Row: min(first, input.LineCount()-1), Col: 0})
	firstNonblank(input)
	normalCursor(input)
	e.finish(input)
}

func (e *Editor) operate(input *textarea.Model, start, end position, linewise bool) {
	if e.op != "y" {
		e.begin(input)
	}
	selectRange(input, start, end)
	if input.HasSelection() {
		e.register = register{text: input.SelectedText(), linewise: linewise}
	}
	if e.op == "y" {
		input.ClearSelection()
		moveCursor(input, start)
		e.mode = Normal
		normalCursor(input)
		e.clearPending()
		return
	}
	input.DeleteSelection()
	input.ClearSelection()
	moveCursor(input, start)
	if e.op == "c" {
		e.enterInsert(input, 1)
		return
	}
	e.mode = Normal
	normalCursor(input)
	e.finish(input)
}

func (e *Editor) put(input *textarea.Model, before bool, count int) {
	if e.register.text == "" {
		return
	}
	// Ordinary counts work even on an empty body; enormous repetition is
	// limited by the body/register size plus a small fixed allowance.
	count = min(count, max(1024, input.Length()+len([]rune(e.register.text))+1))
	text := strings.Repeat(e.register.text, count)
	p := cursor(input)
	if e.register.linewise {
		if before {
			input.CursorStart()
			input.InsertString(text)
			moveCursor(input, position{Row: p.Row, Col: 0})
		} else {
			input.CursorEnd()
			input.InsertString("\n" + strings.TrimSuffix(text, "\n"))
			moveCursor(input, position{Row: p.Row + 1, Col: 0})
		}
		firstNonblank(input)
	} else {
		if !before {
			input.SetCursorColumn(min(p.Col+1, lineLength(input)))
		}
		input.InsertString(text)
		input.SetCursorColumn(max(0, input.Column()-1))
	}
	normalCursor(input)
}

func (e *Editor) visualRange(input *textarea.Model) (position, position) {
	start, end := ordered(e.anchor, cursor(input))
	d := readDocument(input)
	if e.mode == VisualLine {
		start.Col = 0
		if end.Row+1 < len(d.starts) {
			end = position{Row: end.Row + 1, Col: 0}
		} else {
			end.Col = d.end(end.Row) - d.starts[end.Row]
		}
	} else {
		offset := d.offset(end)
		if offset < len(d.runes) {
			end = d.position(offset + 1)
		}
	}
	return start, end
}

func (e *Editor) updateVisual(input *textarea.Model) {
	p := cursor(input)
	start, end := e.visualRange(input)
	selectRange(input, start, end)
	moveCursor(input, p)
}

func (e *Editor) visualKey(input *textarea.Model, key string, count int, explicit bool) {
	if motion(input, key, count, explicit) {
		e.count = 0
		e.updateVisual(input)
		return
	}
	switch key {
	case "v", "V":
		mode := Visual
		if key == "V" {
			mode = VisualLine
		}
		if e.mode == mode {
			e.mode = Normal
			input.ClearSelection()
			e.clearPending()
			return
		}
		e.mode = mode
		e.count = 0
		e.updateVisual(input)
	case "d", "x", "c", "s", "y":
		e.op = key
		if key == "x" {
			e.op = "d"
		} else if key == "s" {
			e.op = "c"
		}
		if e.mode == VisualLine {
			first, last := ordered(e.anchor, cursor(input))
			e.lineOperation(input, first.Row, last.Row)
		} else {
			start, end := e.visualRange(input)
			e.operate(input, start, end, false)
		}
	case "p", "P":
		if e.register.text == "" {
			return
		}
		e.begin(input)
		start, end := e.visualRange(input)
		if e.mode == VisualLine {
			first, last := ordered(e.anchor, cursor(input))
			d := readDocument(input)
			start = position{Row: first.Row, Col: 0}
			end = d.position(d.end(last.Row))
		}
		selectRange(input, start, end)
		replaced := register{text: input.SelectedText(), linewise: e.mode == VisualLine}
		if replaced.linewise && !strings.HasSuffix(replaced.text, "\n") {
			replaced.text += "\n"
		}
		input.DeleteSelection()
		moveCursor(input, start)
		count = min(count, max(1024, input.Length()+len([]rune(e.register.text))+1))
		text := strings.Repeat(e.register.text, count)
		if e.register.linewise {
			text = strings.TrimSuffix(text, "\n")
			if e.mode != VisualLine {
				text = "\n" + text + "\n"
			}
		}
		input.InsertString(text)
		if e.register.linewise {
			row := start.Row
			if e.mode != VisualLine {
				row++
			}
			moveCursor(input, position{Row: row, Col: 0})
			firstNonblank(input)
		} else {
			input.SetCursorColumn(max(0, input.Column()-1))
		}
		if key == "p" {
			e.register = replaced
		}
		e.mode = Normal
		input.ClearSelection()
		normalCursor(input)
		e.finish(input)
	default:
		e.count = 0
	}
}
