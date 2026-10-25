package vimedit

import (
	"unicode"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

type position = textarea.Position

func cursor(input *textarea.Model) position {
	return position{Row: input.Line(), Col: input.Column()}
}

// CursorUp/Down move through display rows, not logical lines. Starting at the
// appropriate logical edge guarantees that each call crosses a logical newline.
func moveCursor(input *textarea.Model, p position) {
	p.Row = max(0, min(p.Row, input.LineCount()-1))
	for input.Line() < p.Row {
		input.CursorEnd()
		input.CursorDown()
	}
	for input.Line() > p.Row {
		input.CursorStart()
		input.CursorUp()
	}
	input.SetCursorColumn(p.Col)
}

func lineLength(input *textarea.Model) int {
	col := input.Column()
	input.CursorEnd()
	n := input.Column()
	input.SetCursorColumn(col)
	return n
}

func normalCursor(input *textarea.Model) {
	input.SetCursorColumn(min(input.Column(), max(0, lineLength(input)-1)))
}

func before(a, b position) bool {
	return a.Row < b.Row || a.Row == b.Row && a.Col < b.Col
}

func ordered(a, b position) (position, position) {
	if before(b, a) {
		return b, a
	}
	return a, b
}

// selectRange uses the widget's public keyboard-selection API. There is no
// public logical SelectRange setter in Bubbles. It deliberately does not replace
// the widget or its value, and its cursor movement is always rune-indexed.
func selectRange(input *textarea.Model, start, end position) {
	input.ClearSelection()
	moveCursor(input, start)
	if start == end {
		return
	}
	advanceSelection := func() {
		updated, _ := input.Update(tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModShift})
		*input = updated
	}
	advanceSelection()
	if cursor(input) == end {
		return
	}
	// Establish the anchor once, then update the head at the range end.
	// Moving the cursor directly preserves the widget's selection anchor.
	previous := end
	if previous.Col > 0 {
		previous.Col--
		moveCursor(input, previous)
	} else {
		moveCursor(input, position{Row: previous.Row - 1, Col: 0})
		input.CursorEnd()
	}
	advanceSelection()
}

// document is a short-lived view for word motions and operator ranges, never a
// second editable buffer. Simple cursor motions don't construct it.
type document struct {
	runes  []rune
	starts []int
}

func readDocument(input *textarea.Model) document {
	d := document{runes: []rune(input.Value()), starts: []int{0}}
	for i, r := range d.runes {
		if r == '\n' {
			d.starts = append(d.starts, i+1)
		}
	}
	return d
}

func (d document) offset(p position) int {
	row := max(0, min(p.Row, len(d.starts)-1))
	return min(d.starts[row]+max(0, p.Col), d.end(row))
}

func (d document) end(row int) int {
	if row+1 < len(d.starts) {
		return d.starts[row+1] - 1
	}
	return len(d.runes)
}

func (d document) position(offset int) position {
	offset = max(0, min(offset, len(d.runes)))
	// Binary search avoids scanning every earlier line for each motion.
	lo, hi := 0, len(d.starts)
	for lo+1 < hi {
		mid := (lo + hi) / 2
		if d.starts[mid] <= offset {
			lo = mid
		} else {
			hi = mid
		}
	}
	return position{Row: lo, Col: offset - d.starts[lo]}
}

func wordClass(r rune, big bool) int {
	if unicode.IsSpace(r) {
		return 0
	}
	if big || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) || r == '_' {
		return 1
	}
	return 2
}

func (d document) word(offset, count int, motion string) int {
	big := motion == "W" || motion == "B" || motion == "E"
	n := len(d.runes)
	count = min(count, n+1)
	for range count {
		previous := offset
		switch motion {
		case "w", "W":
			if offset < n {
				class := wordClass(d.runes[offset], big)
				for offset < n && wordClass(d.runes[offset], big) == class {
					offset++
				}
				for offset < n && wordClass(d.runes[offset], big) == 0 {
					offset++
				}
			}
		case "b", "B":
			if offset > 0 {
				offset--
				for offset > 0 && wordClass(d.runes[offset], big) == 0 {
					offset--
				}
				class := wordClass(d.runes[offset], big)
				for offset > 0 && wordClass(d.runes[offset-1], big) == class {
					offset--
				}
			}
		case "e", "E":
			if offset+1 < n {
				offset++
				for offset < n-1 && wordClass(d.runes[offset], big) == 0 {
					offset++
				}
				class := wordClass(d.runes[offset], big)
				for offset+1 < n && wordClass(d.runes[offset+1], big) == class {
					offset++
				}
			}
		}
		if offset == previous {
			break
		}
	}
	return offset
}

func (d document) innerWord(offset, count int, big bool) (int, int) {
	if len(d.runes) == 0 || offset >= len(d.runes) || d.runes[offset] == '\n' {
		return offset, offset
	}
	class := wordClass(d.runes[offset], big)
	start, end := offset, offset+1
	for start > 0 && d.runes[start-1] != '\n' && wordClass(d.runes[start-1], big) == class {
		start--
	}
	for end < len(d.runes) && d.runes[end] != '\n' && wordClass(d.runes[end], big) == class {
		end++
	}
	for i := 1; i < min(count, len(d.runes)+1) && end < len(d.runes); i++ {
		for end < len(d.runes) && unicode.IsSpace(d.runes[end]) {
			end++
		}
		if end < len(d.runes) {
			class = wordClass(d.runes[end], big)
			for end < len(d.runes) && wordClass(d.runes[end], big) == class {
				end++
			}
		}
	}
	return start, end
}

func firstNonblank(input *textarea.Model) {
	d := readDocument(input)
	row := input.Line()
	i := d.starts[row]
	for i < d.end(row) && unicode.IsSpace(d.runes[i]) {
		i++
	}
	input.SetCursorColumn(i - d.starts[row])
}

func motion(input *textarea.Model, key string, count int, explicit bool) bool {
	p := cursor(input)
	switch key {
	case "h", "left":
		input.SetCursorColumn(max(0, p.Col-count))
	case "l", "right":
		input.SetCursorColumn(min(max(0, lineLength(input)-1), p.Col+count))
	case "j", "down":
		moveCursor(input, position{Row: p.Row + min(count, input.LineCount()), Col: p.Col})
	case "k", "up":
		moveCursor(input, position{Row: p.Row - min(count, input.LineCount()), Col: p.Col})
	case "0", "home":
		input.CursorStart()
	case "^":
		firstNonblank(input)
	case "$", "end":
		moveCursor(input, position{Row: p.Row + min(count-1, input.LineCount()), Col: 0})
		input.CursorEnd()
	case "gg":
		moveCursor(input, position{Row: count - 1, Col: 0})
		firstNonblank(input)
	case "G":
		row := input.LineCount() - 1
		if explicit {
			row = count - 1
		}
		moveCursor(input, position{Row: row, Col: 0})
		firstNonblank(input)
	case "w", "b", "e", "W", "B", "E":
		d := readDocument(input)
		moveCursor(input, d.position(d.word(d.offset(p), count, key)))
	default:
		return false
	}
	normalCursor(input)
	return true
}
