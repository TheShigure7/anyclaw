package web

import (
	"strings"
	"testing"
)

func TestBuildSearchURLEscapesQuery(t *testing.T) {
	url := buildSearchURL("golang tips & tricks")

	if !strings.HasPrefix(url, SearchEndpointURL()+"?q=") {
		t.Fatalf("expected search endpoint prefix, got %q", url)
	}
	if strings.Contains(url, "tips & tricks") {
		t.Fatalf("expected query to be escaped, got %q", url)
	}
	if !strings.Contains(url, "tips+%26+tricks") {
		t.Fatalf("expected escaped query string, got %q", url)
	}
}
