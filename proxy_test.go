package main

import "testing"

func TestParseKentaaIDs(t *testing.T) {
	page := []byte(`<html><body data-site-id="Dp9tjHj3gsVn" data-project-id="TRFw6r4CiRFm" data-action-id="znQjamA72ySe"><div data-site-id="nope"></div></body>`)
	site, action, ok := parseKentaaIDs(page)
	if !ok || site != "Dp9tjHj3gsVn" || action != "znQjamA72ySe" {
		t.Fatalf("got %q %q %v", site, action, ok)
	}
	if _, _, ok := parseKentaaIDs([]byte(`<body data-site-id="x">`)); ok {
		t.Fatal("expected failure without action id")
	}
}
