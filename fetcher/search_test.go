package fetcher

import (
	"slices"
	"testing"
	"time"

	"github.com/floatpane/matcha/backend"
)

func TestBuildSearchCriteria(t *testing.T) {
	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	c := buildSearchCriteria(backend.SearchQuery{
		From: "alice@example.com", To: "bob@example.com", Subject: "invoice",
		Body: "paid", Since: since, Before: before, LargerThan: 4096,
	})

	if len(c.Header) != 3 || c.Header[0].Key != "From" || c.Header[1].Key != "To" || c.Header[2].Key != "Subject" {
		t.Fatalf("headers = %+v", c.Header)
	}
	if len(c.Body) != 1 || c.Body[0] != "paid" || len(c.Text) != 0 || !c.Since.Equal(since) || !c.Before.Equal(before) || c.Larger != 4096 {
		t.Fatalf("criteria = %+v", c)
	}
	if searchLimit(backend.SearchQuery{}) != 100 || searchLimit(backend.SearchQuery{Limit: 25}) != 25 {
		t.Fatal("unexpected search limit")
	}
}

func TestBuildSearchCriteriaText(t *testing.T) {
	c := buildSearchCriteria(backend.ParseSearchQuery(`march invoice "quarterly report"`))
	if !slices.Equal(c.Text, []string{"march", "invoice", "quarterly report"}) || len(c.Body) != 0 {
		t.Fatalf("Text = %q Body = %q, want one TEXT key per word, the quoted phrase kept whole, and no BODY", c.Text, c.Body)
	}

	c = buildSearchCriteria(backend.ParseSearchQuery("body:paid invoice"))
	if len(c.Body) != 1 || c.Body[0] != "paid" || len(c.Text) != 1 || c.Text[0] != "invoice" {
		t.Fatalf("Body = %q Text = %q, want BODY paid and TEXT invoice", c.Body, c.Text)
	}
}
