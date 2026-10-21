package backend

import (
	"testing"
	"time"
)

func TestParseSearchQuery(t *testing.T) {
	q := ParseSearchQuery(`from:alice@example.com to:bob@example.com subject:report body:revenue since:2026-01-01 before:2026-02-01 larger:10240`)
	if q.From != "alice@example.com" || q.To != "bob@example.com" || q.Subject != "report" || q.Body != "revenue" || q.Text != "" {
		t.Fatalf("parsed fields = %+v", q)
	}
	if !q.Since.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) || !q.Before.Equal(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("parsed dates = since:%v before:%v", q.Since, q.Before)
	}
	if q.LargerThan != 10240 || q.Raw == "" {
		t.Fatalf("parsed size/raw = larger:%d raw:%q", q.LargerThan, q.Raw)
	}
}

func TestParseSearchQueryBareTerms(t *testing.T) {
	q := ParseSearchQuery("quarterly revenue update")
	if q.Text != "quarterly revenue update" || q.Body != "" {
		t.Fatalf("Text = %q Body = %q, want Text quarterly revenue update and empty Body", q.Text, q.Body)
	}
	q = ParseSearchQuery("from:alice@example.com quarterly revenue")
	if q.Text != "quarterly revenue" || q.Body != "" {
		t.Fatalf("fielded search Text = %q Body = %q, want Text quarterly revenue and empty Body", q.Text, q.Body)
	}
	q = ParseSearchQuery("unknown:value plain")
	if q.Text != "unknown:value plain" {
		t.Fatalf("unknown key Text = %q, want unknown:value plain", q.Text)
	}
}

func TestParseSearchQueryQuotedValues(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		from    string
		subject string
		body    string
		text    string
	}{
		{
			name:    "double quoted subject",
			input:   `subject:"quarterly report"`,
			subject: "quarterly report",
		},
		{
			name:  "bare terms after field",
			input: `from:alice quarterly revenue`,
			from:  "alice",
			text:  "quarterly revenue",
		},
		{
			name:  "body prefix and bare terms are kept apart",
			input: `body:foo bar baz`,
			body:  "foo",
			text:  "bar baz",
		},
		{
			name:    "single quoted subject",
			input:   `subject:'quarterly report'`,
			subject: "quarterly report",
		},
		{
			name:    "mixed quoted and unquoted",
			input:   `from:alice subject:"quarterly report" revenue`,
			from:    "alice",
			subject: "quarterly report",
			text:    "revenue",
		},
		{
			name:  "quoted bare phrase",
			input: `"quarterly report"`,
			text:  "quarterly report",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := ParseSearchQuery(tt.input)
			if q.From != tt.from || q.Subject != tt.subject || q.Body != tt.body || q.Text != tt.text {
				t.Fatalf("ParseSearchQuery(%q) = From:%q Subject:%q Body:%q Text:%q, want From:%q Subject:%q Body:%q Text:%q",
					tt.input, q.From, q.Subject, q.Body, q.Text, tt.from, tt.subject, tt.body, tt.text)
			}
		})
	}
}
