package main

import (
	"path/filepath"
	"testing"
)

func TestParseKentaaIDs(t *testing.T) {
	page := []byte(`<html><body data-site-id="Dp9tjHj3gsVn" data-project-id="TRFw6r4CiRFm" data-action-id="znQjamA72ySe"><div data-site-id="nope"></div></body>`)
	site, path, ok := parseKentaaIDs(page)
	if !ok || site != "Dp9tjHj3gsVn" || path != "actions/znQjamA72ySe" {
		t.Fatalf("got %q %q %v", site, path, ok)
	}
	team := []byte(`<body data-site-id="o9vDUSYsMfDp" data-project-id="cWqc9naip75k" data-team-id="oE34ygBfseF3">`)
	if _, path, _ := parseKentaaIDs(team); path != "teams/oE34ygBfseF3" {
		t.Fatalf("team: got %q", path)
	}
	project := []byte(`<body data-site-id="o9vDUSYsMfDp" data-project-id="cWqc9naip75k">`)
	if _, path, _ := parseKentaaIDs(project); path != "projects/cWqc9naip75k" {
		t.Fatalf("project: got %q", path)
	}
	if _, _, ok := parseKentaaIDs([]byte(`<body data-site-id="x">`)); ok {
		t.Fatal("expected failure without action id")
	}
}

func TestParseSecurycast(t *testing.T) {
	page := []byte(`<body data-page="box-page"><div class="text-xl">17&#xA0;167,50 &euro; kerätty</div>
		<span>43%</span>
		<span>Tavoite: 40&#xA0;000 &euro;</span>`)
	got, ok := parseSecurycast(page)
	if !ok || got != (ProxyResult{TargetAmount: 40000, TotalAmount: "17167.50"}) {
		t.Fatalf("got %+v %v", got, ok)
	}
	got, ok = parseSecurycast([]byte(`<div>1 585 € kerätty</div>`))
	if !ok || got != (ProxyResult{TotalAmount: "1585"}) {
		t.Fatalf("no goal: got %+v %v", got, ok)
	}
}

func TestParseMielipotti(t *testing.T) {
	page := []byte(`<div id="app" data-page="{&quot;component&quot;:&quot;Fundraisers/Show&quot;,&quot;props&quot;:{&quot;fundraiser&quot;:{&quot;goal&quot;:4000,&quot;current&quot;:3435}}}"></div>`)
	got, ok := parseMielipotti(page)
	if !ok || got != (ProxyResult{TargetAmount: 4000, TotalAmount: "3435"}) {
		t.Fatalf("got %+v %v", got, ok)
	}
	if _, ok := parseMielipotti([]byte(`<body data-page="box-page">`)); ok {
		t.Fatal("expected failure on non-Inertia page")
	}
}

func TestParseNenapaiva(t *testing.T) {
	page := []byte("<div class=\"flex-basis-12 mb-5 nettilipas-react\"\n\t\tdata-sum='1&nbsp;381'\n\t\tdata-goal='2&nbsp;000'>")
	got, ok := parseNenapaiva(page)
	if !ok || got != (ProxyResult{TargetAmount: 2000, TotalAmount: "1381"}) {
		t.Fatalf("got %+v %v", got, ok)
	}
}

func TestStatsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats.json")
	pollStats = map[string]*pollStat{}
	if err := loadStats(path); err != nil {
		t.Fatalf("missing file: %v", err)
	}
	countPoll("a")
	countPoll("a")
	if err := saveStats(path); err != nil {
		t.Fatal(err)
	}
	pollStats = map[string]*pollStat{}
	if err := loadStats(path); err != nil {
		t.Fatal(err)
	}
	countPoll("a")
	if got := pollStats["a"].Polls; got != 3 {
		t.Fatalf("got %d polls, want 3", got)
	}
}
