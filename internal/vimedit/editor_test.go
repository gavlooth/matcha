package vimedit_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/floatpane/matcha/internal/vimedit"
)

func body(value string) textarea.Model {
	input := textarea.New()
	input.CharLimit = 0
	input.SetWidth(40)
	input.SetHeight(5)
	input.SetValue(value)
	input.MoveToBegin()
	input.Focus()
	return input
}

func press(editor *vimedit.Editor, input *textarea.Model, keys string) {
	for _, r := range keys {
		editor.Update(input, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func escape(editor *vimedit.Editor, input *textarea.Model) {
	editor.Update(input, tea.KeyPressMsg{Code: tea.KeyEscape})
}

func assertBody(t *testing.T, input *textarea.Model, want string) {
	t.Helper()
	if got := input.Value(); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
	if !utf8.ValidString(input.Value()) {
		t.Fatal("editor produced invalid UTF-8")
	}
}

func TestNormalEdits(t *testing.T) {
	tests := []struct {
		name  string
		value string
		keys  string
		want  string
	}{
		{"delete rune", "α🙂β", "x", "🙂β"},
		{"delete previous", "α🙂β", "lX", "🙂β"},
		{"delete previous at start", "α🙂β", "X", "α🙂β"},
		{"delete count", "α🙂βγ", "2x", "βγ"},
		{"delete count stays on line", "αβ\nγδ", "99x", "\nγδ"},
		{"delete to end", "αβγ\nδε", "lD", "α\nδε"},
		{"delete line", "αβ\nγδ\nεζ", "dd", "γδ\nεζ"},
		{"delete final line", "αβ\nγδ", "Gdd", "αβ"},
		{"delete trailing empty line", "αβ\n", "Gdd", "αβ"},
		{"delete empty first line", "\nαβ", "dd", "αβ"},
		{"delete all lines", "αβ\nγδ", "9dd", ""},
		{"delete only line", "αβ", "dd", ""},
		{"delete empty body", "", "ddxXD", ""},
		{"delete word", "ένα δύο τρία", "dw", "δύο τρία"},
		{"delete word end", "ένα δύο", "de", " δύο"},
		{"delete dollar", "ένα δύο\nτρία", "wd$", "ένα \nτρία"},
		{"delete zero", "ένα δύο", "wd0", "δύο"},
		{"delete backward word", "ένα δύο", "wdb", "δύο"},
		{"delete inner word", "ένα δύο τρία", "wldiw", "ένα  τρία"},
		{"delete punctuation word", "one, two", "wdiw", "one two"},
		{"operator counts multiply", "a b c d e f g h", "2d3w", "g h"},
		{"line counts multiply", "1\n2\n3\n4\n5\n6\n7", "2d3d", "7"},
		{"delete to last line", "1\n2\n3", "jdG", "1"},
		{"delete to numbered line", "1\n2\n3\n4", "d2G", "3\n4"},
		{"delete to first line", "1\n2\n3", "Gdgg", ""},
		{"word stops before newline", "one\ntwo", "dw", "\ntwo"},
		{"character paste after", "α🙂β", "ylp", "αα🙂β"},
		{"character paste before", "α🙂β", "ylP", "αα🙂β"},
		{"counted character paste", "αβ", "yl3p", "ααααβ"},
		{"inner word yank", "ένα δύο", "yiwwP", "ένα έναδύο"},
		{"line paste after", "αβ\nγδ", "yyp", "αβ\nαβ\nγδ"},
		{"line paste before", "αβ\nγδ", "yyjP", "αβ\nαβ\nγδ"},
		{"line paste at end", "αβ\nγδ", "yyGp", "αβ\nγδ\nαβ"},
		{"delete paste only line", "αβ", "ddp", "\nαβ"},
		{"line register count", "α\nβ", "yy2p", "α\nα\nα\nβ"},
		{"paste preserves trailing empty", "α\n", "yyp", "α\nα\n"},
		{"empty register is inert", "abc", "pP", "abc"},
		{"unsupported actions inert", "abc", ":qZZ", "abc"},
		{"large counts stop at body end", "α🙂β\nγ", strings.Repeat("9", 80) + "d" + strings.Repeat("9", 80) + "w", ""},
		{"yank word motion", "one two", "ywwP", "one one two"},
		{"yank word end", "one two", "ye$p", "one twoone"},
		{"yank line end", "α🙂β", "y$P", "α🙂βα🙂β"},
		{"yank line start", "one two", "wy0P", "one one two"},
		{"delete big inner word", "one,two three", "diW", " three"},
		{"counted inner word", "a b c", "2diw", " c"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := body(tt.value)
			editor := vimedit.New()
			press(editor, &input, tt.keys)
			assertBody(t, &input, tt.want)
			if editor.Mode() != vimedit.Normal {
				t.Fatalf("mode = %v", editor.Mode())
			}
		})
	}
}

func TestMotions(t *testing.T) {
	tests := []struct {
		keys string
		row  int
		col  int
	}{
		{"l", 0, 1},
		{"99l", 0, 12},
		{"$", 0, 12},
		{"$0", 0, 0},
		{"^", 0, 2},
		{"w", 0, 2},
		{"2w", 0, 6},
		{"3w", 0, 8},
		{"W", 0, 2},
		{"2W", 0, 8},
		{"e", 0, 5},
		{"E", 0, 6},
		{"2eb", 0, 2},
		{"2WB", 0, 2},
		{"j", 1, 0},
		{"2j", 2, 0},
		{"G", 3, 0},
		{"2G", 1, 0},
		{"Ggg", 0, 2},
		{"3gg", 2, 0},
		{"999k", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.keys, func(t *testing.T) {
			input := body("  ένας, δύο🙂!\nβ\nγδ\n")
			editor := vimedit.New()
			press(editor, &input, tt.keys)
			if input.Line() != tt.row || input.Column() != tt.col {
				t.Fatalf("cursor = (%d,%d), want (%d,%d)", input.Line(), input.Column(), tt.row, tt.col)
			}
		})
	}
}

func TestInsertAndChangeCommands(t *testing.T) {
	tests := []struct {
		name  string
		value string
		keys  string
		text  string
		want  string
	}{
		{"insert", "αβ", "i", "🙂", "🙂αβ"},
		{"first nonblank", "  αβ", "$I", "🙂", "  🙂αβ"},
		{"append", "αβ", "a", "🙂", "α🙂β"},
		{"append end", "αβ", "A", "🙂", "αβ🙂"},
		{"open below", "αβ\nγδ", "o", "🙂", "αβ\n🙂\nγδ"},
		{"open above", "αβ\nγδ", "O", "🙂", "🙂\nαβ\nγδ"},
		{"open final empty", "αβ\n", "Go", "🙂", "αβ\n\n🙂"},
		{"counted open below", "αβ\nγδ", "3o", "🙂", "αβ\n🙂\n🙂\n🙂\nγδ"},
		{"counted open above", "αβ\nγδ", "3O", "🙂", "🙂\n🙂\n🙂\nαβ\nγδ"},
		{"substitute", "α🙂β", "ls", "γ", "αγβ"},
		{"substitute count", "α🙂βγ", "2s", "δ", "δβγ"},
		{"change end", "αβγ\nδε", "lC", "🙂", "α🙂\nδε"},
		{"change inner word", "ένα δύο τρία", "wlciw", "🙂", "ένα 🙂 τρία"},
		{"change word", "ένα δύο", "cw", "🙂", "🙂 δύο"},
		{"change single-rune word", "α β", "cw", "🙂", "🙂 β"},
		{"change multiple words", "α β γ", "2cw", "🙂", "🙂 γ"},
		{"change line", "αβ\nγδ", "cc", "🙂", "🙂\nγδ"},
		{"change final line", "αβ\nγδ", "Gcc", "🙂", "αβ\n🙂"},
		{"change whole line shortcut", "αβ\nγδ", "S", "🙂", "🙂\nγδ"},
		{"change multiple lines", "α\nβ\nγ", "2cc", "🙂", "🙂\nγ"},
		{"insert repeat count", "α", "3i", "🙂", "🙂🙂🙂α"},
		{"empty substitution", "", "s", "🙂", "🙂"},
		{"change dollar operator", "αβγ\nδε", "lc$", "🙂", "α🙂\nδε"},
		{"change zero operator", "ένα δύο", "wc0", "🙂", "🙂δύο"},
		{"visual line change", "αβ\nγδ", "Vc", "🙂", "🙂\nγδ"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := body(tt.value)
			editor := vimedit.New()
			press(editor, &input, tt.keys)
			if editor.Mode() != vimedit.Insert {
				t.Fatalf("mode = %v, want insert", editor.Mode())
			}
			press(editor, &input, tt.text)
			escape(editor, &input)
			assertBody(t, &input, tt.want)
			press(editor, &input, "u")
			assertBody(t, &input, tt.value)
			editor.Update(&input, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
			assertBody(t, &input, tt.want)
		})
	}
}

func TestVisualRanges(t *testing.T) {
	tests := []struct {
		name  string
		value string
		keys  string
		want  string
	}{
		{"inclusive forward", "α🙂βγ", "vld", "βγ"},
		{"inclusive reverse", "α🙂βγ", "llvhhd", "γ"},
		{"reverse across newline", "αβ\n🙂γ", "jlvk0d", ""},
		{"linewise", "α\nβ\nγ", "Vjd", "γ"},
		{"linewise reverse", "α\nβ\nγ", "jVkd", "γ"},
		{"linewise last", "α\nβ\nγ", "GVd", "α\nβ"},
		{"linewise empty final", "α\n", "GVd", "α"},
		{"visual yank", "α🙂βγ", "vly$p", "α🙂βγα🙂"},
		{"character replacement", "α🙂βγ", "ylwvlp", "ααγ"},
		{"line replacement", "α\nβ\nγ", "yyjVp", "α\nα\nγ"},
		{"replace final lines", "α\nβ\nγ", "yyjVjp", "α\nα"},
		{"replace all lines", "α\nβ", "yyVjp", "α"},
		{"character replace visual lines", "α\nβ\nγ", "yljVp", "α\nα\nγ"},
		{"visual empty delete", "", "vd", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := body(tt.value)
			editor := vimedit.New()
			press(editor, &input, tt.keys)
			assertBody(t, &input, tt.want)
		})
	}
}

func TestVisualPasteRegister(t *testing.T) {
	for _, test := range []struct {
		keys  string
		value string
		want  string
	}{
		{"yiwwvepwP", "one two three", "one one twothree"},
		{"yiwwvePwP", "one two three", "one one onethree"},
		{"yyjVpp", "α\nβ\nγ", "α\nα\nβ\nγ"},
		{"yyjVPp", "α\nβ\nγ", "α\nα\nα\nγ"},
	} {
		t.Run(test.keys, func(t *testing.T) {
			input := body(test.value)
			editor := vimedit.New()
			press(editor, &input, test.keys)
			assertBody(t, &input, test.want)
		})
	}
}

func TestVisualSelectionAndCancel(t *testing.T) {
	input := body("α🙂β\nγδ")
	editor := vimedit.New()
	press(editor, &input, "llvh")
	if got := input.SelectedText(); got != "🙂β" {
		t.Fatalf("reverse selection = %q", got)
	}
	if editor.Mode() != vimedit.Visual {
		t.Fatal("not in visual mode")
	}
	escape(editor, &input)
	if input.HasSelection() || editor.Mode() != vimedit.Normal {
		t.Fatal("escape did not clear visual mode/selection")
	}
	assertBody(t, &input, "α🙂β\nγδ")
	press(editor, &input, "V")
	if editor.Mode() != vimedit.VisualLine {
		t.Fatal("not in visual-line mode")
	}
	editor.Leave(&input)
	if input.HasSelection() || editor.Mode() != vimedit.Normal {
		t.Fatal("Leave did not clear visual-line mode/selection")
	}
}

func TestVisualChangeUndo(t *testing.T) {
	input := body("α🙂βγ")
	editor := vimedit.New()
	press(editor, &input, "llvhhcδε")
	escape(editor, &input)
	assertBody(t, &input, "δεγ")
	press(editor, &input, "u")
	assertBody(t, &input, "α🙂βγ")
}

func TestUndoInsertSessionAndRedoBranch(t *testing.T) {
	input := body("αβ")
	editor := vimedit.New()
	press(editor, &input, "iγδ")
	editor.Update(&input, tea.KeyPressMsg{Code: tea.KeyBackspace})
	press(editor, &input, "🙂")
	editor.Update(&input, tea.KeyPressMsg{Code: tea.KeyEnter})
	press(editor, &input, "ε")
	escape(editor, &input)
	assertBody(t, &input, "γ🙂\nεαβ")
	press(editor, &input, "u")
	assertBody(t, &input, "αβ")
	editor.Update(&input, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	assertBody(t, &input, "γ🙂\nεαβ")
	press(editor, &input, "u")
	press(editor, &input, "x")
	editor.Update(&input, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	assertBody(t, &input, "β")
}

func TestDotRepeat(t *testing.T) {
	tests := []struct {
		value  string
		before string
		insert string
		after  string
		want   string
	}{
		{"αβγδ", "x", "", ".", "γδ"},
		{"α β γ δ", "dw", "", ".", "γ δ"},
		{"α β γ", "ciw", "🙂", "w.", "🙂 🙂 γ"},
		{"αβ", "a", "🙂", ".", "α🙂🙂β"},
		{"αβγδ", "vlx", "", ".", ""},
	}
	for _, tt := range tests {
		t.Run(tt.before+tt.after, func(t *testing.T) {
			input := body(tt.value)
			editor := vimedit.New()
			press(editor, &input, tt.before)
			if editor.Mode() == vimedit.Insert {
				press(editor, &input, tt.insert)
				escape(editor, &input)
			}
			press(editor, &input, tt.after)
			assertBody(t, &input, tt.want)
		})
	}
}

func TestLiteralBracketedPaste(t *testing.T) {
	for _, insert := range []bool{false, true} {
		input := body("αβ")
		editor := vimedit.New()
		if insert {
			press(editor, &input, "i")
		}
		editor.Update(&input, tea.PasteStartMsg{})
		editor.Update(&input, tea.PasteMsg{Content: "dd:q!🙂\nγ"})
		editor.Update(&input, tea.PasteEndMsg{})
		if insert {
			escape(editor, &input)
		}
		assertBody(t, &input, "dd:q!🙂\nγαβ")
		press(editor, &input, "u")
		assertBody(t, &input, "αβ")
		editor.Update(&input, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
		assertBody(t, &input, "dd:q!🙂\nγαβ")
	}
}

func TestLeaveAndReset(t *testing.T) {
	input := body("αβ")
	editor := vimedit.New()
	press(editor, &input, "i🙂")
	editor.Leave(&input)
	if editor.Mode() != vimedit.Normal {
		t.Fatal("Leave did not enter normal")
	}
	assertBody(t, &input, "🙂αβ")
	press(editor, &input, "u")
	assertBody(t, &input, "αβ")
	press(editor, &input, "d")
	editor.Leave(&input)
	press(editor, &input, "w")
	assertBody(t, &input, "αβ")
	editor.Reset()
	press(editor, &input, "uP.")
	assertBody(t, &input, "αβ")
	press(editor, &input, "iγ")
	editor.Update(&input, tea.KeyPressMsg{Code: '[', Mod: tea.ModCtrl})
	if editor.Mode() != vimedit.Normal {
		t.Fatal("Ctrl+[ did not exit insert")
	}
	before := input.Value()
	editor.Reset()
	if editor.Mode() != vimedit.Normal || input.Value() != before {
		t.Fatal("Reset changed widget or did not reset mode")
	}
	press(editor, &input, "u")
	assertBody(t, &input, before)
}

func TestCancelledPrefixesAndNoopUndo(t *testing.T) {
	input := body("αβ\nγδ")
	editor := vimedit.New()
	for _, prefix := range []string{"d", "2d3", "g", "di", "999"} {
		press(editor, &input, prefix)
		escape(editor, &input)
		press(editor, &input, "w")
		assertBody(t, &input, "αβ\nγδ")
		press(editor, &input, "gg0")
	}
	press(editor, &input, "x")
	press(editor, &input, "i")
	escape(editor, &input)
	press(editor, &input, "u")
	assertBody(t, &input, "αβ\nγδ")
}

func TestWrappedLogicalRowsAndWidgetProperties(t *testing.T) {
	value := "α🙂βγδεζηθικλμνξοπρστυφχψω\nsecond\nthird"
	input := body(value)
	input.ShowLineNumbers = false
	input.Prompt = "mail> "
	input.SetWidth(14)
	input.SetHeight(3)
	styles := input.Styles()
	styles.Focused.Text = styles.Focused.Text.Bold(true)
	input.SetStyles(styles)
	editor := vimedit.New()
	width, height := input.Width(), input.Height()
	press(editor, &input, "j")
	if input.Line() != 1 || input.Column() != 0 {
		t.Fatalf("j crossed display row, not logical row: (%d,%d)", input.Line(), input.Column())
	}
	press(editor, &input, "k$")
	if input.Line() != 0 || input.Column() != len([]rune(strings.Split(value, "\n")[0]))-1 {
		t.Fatalf("$ did not reach logical line end: (%d,%d)", input.Line(), input.Column())
	}
	press(editor, &input, "0dd")
	assertBody(t, &input, "second\nthird")
	press(editor, &input, "u")
	assertBody(t, &input, value)
	if !input.Focused() || input.Width() != width || input.Height() != height || input.Prompt != "mail> " {
		t.Fatal("editing replaced widget configuration")
	}
	if !input.Styles().Focused.Text.GetBold() {
		t.Fatal("editing lost text style")
	}
}

func TestArrowKeys(t *testing.T) {
	input := body("α🙂β\nγδ")
	editor := vimedit.New()
	for _, code := range []rune{tea.KeyRight, tea.KeyDown, tea.KeyLeft, tea.KeyUp} {
		editor.Update(&input, tea.KeyPressMsg{Code: code})
	}
	if input.Line() != 0 || input.Column() != 0 {
		t.Fatalf("arrow cursor = (%d,%d)", input.Line(), input.Column())
	}
	assertBody(t, &input, "α🙂β\nγδ")
}
