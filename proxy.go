package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Kentaa API configuration.
var (
	kentaaURL         = "https://frontend-api.kentaa.nl/actions/xF7BY4oprbNi"
	kentaaHeaderKey   = "x-site-id"
	kentaaHeaderValue = "LqS5hWxATJhq"
)

// Cache variables for Kentaa.
var (
	kentaaCacheData   []byte
	kentaaCacheExpiry time.Time
	kentaaCacheMutex  sync.Mutex
)

var cacheDuration = 5 * time.Second

// KentaaResponse represents the structure of the Kentaa upstream JSON response.
type KentaaResponse struct {
	Data struct {
		TargetAmount int    `json:"target_amount"`
		TotalAmount  string `json:"total_amount"`
	} `json:"data"`
}

// ProxyResult is the structure for our proxied output.
type ProxyResult struct {
	TargetAmount int    `json:"target_amount"`
	TotalAmount  string `json:"total_amount"`
}

func setCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

func kentaaHandler(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)

	// Handle preflight requests.
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Return cached response if valid.
	kentaaCacheMutex.Lock()
	if time.Now().Before(kentaaCacheExpiry) && kentaaCacheData != nil {
		data := kentaaCacheData
		kentaaCacheMutex.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
		return
	}
	kentaaCacheMutex.Unlock()

	// Create request to Kentaa API.
	client := &http.Client{}
	req, err := http.NewRequest("GET", kentaaURL, nil)
	if err != nil {
		http.Error(w, "Error creating request", http.StatusInternalServerError)
		return
	}
	req.Header.Set(kentaaHeaderKey, kentaaHeaderValue)

	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "Error fetching remote data", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Read the upstream response.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "Error reading response", http.StatusInternalServerError)
		return
	}

	// Parse the upstream JSON.
	var upstream KentaaResponse
	if err := json.Unmarshal(body, &upstream); err != nil {
		http.Error(w, "Error parsing upstream JSON", http.StatusInternalServerError)
		return
	}

	// Prepare our proxied response.
	proxyResult := ProxyResult{
		TargetAmount: upstream.Data.TargetAmount,
		TotalAmount:  upstream.Data.TotalAmount,
	}

	finalData, err := json.Marshal(proxyResult)
	if err != nil {
		http.Error(w, "Error creating response JSON", http.StatusInternalServerError)
		return
	}

	// Cache the final result.
	kentaaCacheMutex.Lock()
	kentaaCacheData = finalData
	kentaaCacheExpiry = time.Now().Add(cacheDuration)
	kentaaCacheMutex.Unlock()

	// Return the filtered JSON response.
	w.Header().Set("Content-Type", "application/json")
	w.Write(finalData)
}

// fundraiserEntry holds a fundraiser page's cached result. Kentaa pages also
// remember their site ID and API path so refreshes go straight to the Kentaa API.
type fundraiserEntry struct {
	siteID, kentaaPath string
	data               []byte
	expiry             time.Time
}

// Fundraiser entries keyed by page host+path.
// ponytail: unbounded map, add eviction if it grows too big.
var (
	fundraiserEntries = map[string]fundraiserEntry{}
	fundraiserMutex   sync.Mutex
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

// Kentaa (iRaiser) pages expose their IDs as data attributes on the <body> tag.
var bodyTagRegex = regexp.MustCompile(`<body[^>]*>`)
var kentaaAttrRegex = regexp.MustCompile(`data-(site|action|team|project)-id="([A-Za-z0-9]+)"`)

// Nenäpäivä nettilipas pages carry their amounts as data-sum and data-goal attributes.
var nenapaivaRegex = regexp.MustCompile(`nettilipas-react[^>]*data-sum='([^']*)'[^>]*data-goal='([^']*)'`)

// Mielipotti (Inertia.js) pages embed their data as JSON in a data-page attribute.
var inertiaPageRegex = regexp.MustCompile(`data-page="([^"]*)"`)

// Securycast pages show "17 167 € kerätty" and, when set, "Tavoite: 40 000 €".
const securycastAmount = `((?:\d|[\s\x{00A0}]|&#xA0;|,)+?)[\s\x{00A0}]*(?:€|&euro;)`

var securycastTotalRegex = regexp.MustCompile(securycastAmount + `[\s\x{00A0}]*kerätty`)
var securycastTargetRegex = regexp.MustCompile(`Tavoite:` + securycastAmount)
var amountSpaceRegex = regexp.MustCompile(`[\s\x{00A0}]+|&#xA0;|&nbsp;`)

//go:embed index.html
var indexHTML []byte

//go:embed widget.html
var widgetHTML []byte

// parseKentaaIDs extracts the site ID and API path from a Kentaa page. A
// fundraiser page also names its team and project, so the most specific wins.
func parseKentaaIDs(page []byte) (siteID, path string, ok bool) {
	ids := map[string]string{}
	for _, m := range kentaaAttrRegex.FindAllSubmatch(bodyTagRegex.Find(page), -1) {
		ids[string(m[1])] = string(m[2])
	}
	for _, kind := range []string{"action", "team", "project"} {
		if ids[kind] != "" {
			path = kind + "s/" + ids[kind]
			break
		}
	}
	return ids["site"], path, ids["site"] != "" && path != ""
}

// parseMielipotti reads the amounts from a Mielipotti fundraiser page.
func parseMielipotti(page []byte) (ProxyResult, bool) {
	m := inertiaPageRegex.FindSubmatch(page)
	if m == nil {
		return ProxyResult{}, false
	}
	var data struct {
		Props struct {
			Fundraiser *struct {
				Goal    float64     `json:"goal"`
				Current json.Number `json:"current"`
			} `json:"fundraiser"`
		} `json:"props"`
	}
	if err := json.Unmarshal([]byte(html.UnescapeString(string(m[1]))), &data); err != nil {
		return ProxyResult{}, false
	}
	f := data.Props.Fundraiser
	if f == nil || f.Current == "" {
		return ProxyResult{}, false
	}
	return ProxyResult{TargetAmount: int(f.Goal), TotalAmount: f.Current.String()}, true
}

// parseFinnishAmount turns an amount like "17&#xA0;167,50" into "17167.50".
func parseFinnishAmount(s []byte) (string, float64, bool) {
	amount := strings.Replace(amountSpaceRegex.ReplaceAllString(string(s), ""), ",", ".", 1)
	f, err := strconv.ParseFloat(amount, 64)
	return amount, f, err == nil
}

// parseNenapaiva reads the amounts from a Nenäpäivä nettilipas page.
func parseNenapaiva(page []byte) (ProxyResult, bool) {
	m := nenapaivaRegex.FindSubmatch(page)
	if m == nil {
		return ProxyResult{}, false
	}
	total, _, ok := parseFinnishAmount(m[1])
	if !ok {
		return ProxyResult{}, false
	}
	_, goal, _ := parseFinnishAmount(m[2])
	return ProxyResult{TargetAmount: int(goal), TotalAmount: total}, true
}

// parseSecurycast reads the amounts from a Securycast fundraiser page.
func parseSecurycast(page []byte) (ProxyResult, bool) {
	m := securycastTotalRegex.FindSubmatch(page)
	if m == nil {
		return ProxyResult{}, false
	}
	total, _, ok := parseFinnishAmount(m[1])
	if !ok {
		return ProxyResult{}, false
	}
	result := ProxyResult{TotalAmount: total}
	if m := securycastTargetRegex.FindSubmatch(page); m != nil {
		if _, target, ok := parseFinnishAmount(m[1]); ok {
			result.TargetAmount = int(target)
		}
	}
	return result, true
}

// fetchKentaa reads the amounts of a Kentaa action, team or project from the API.
func fetchKentaa(siteID, path string) (ProxyResult, error) {
	req, err := http.NewRequest("GET", "https://frontend-api.kentaa.nl/"+path, nil)
	if err != nil {
		return ProxyResult{}, err
	}
	req.Header.Set(kentaaHeaderKey, siteID)

	resp, err := httpClient.Do(req)
	if err != nil {
		return ProxyResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ProxyResult{}, fmt.Errorf("Kentaa API returned %d", resp.StatusCode)
	}

	var upstream KentaaResponse
	if err := json.NewDecoder(resp.Body).Decode(&upstream); err != nil {
		return ProxyResult{}, err
	}
	return ProxyResult{TargetAmount: upstream.Data.TargetAmount, TotalAmount: upstream.Data.TotalAmount}, nil
}

// fetchFundraiser reads the amounts of a Kentaa, Mielipotti, Nenäpäivä or
// Securycast fundraiser page, storing Kentaa IDs in entry for later refreshes.
func fetchFundraiser(pageURL string, entry *fundraiserEntry) (ProxyResult, error) {
	if entry.kentaaPath != "" {
		return fetchKentaa(entry.siteID, entry.kentaaPath)
	}

	resp, err := httpClient.Get(pageURL)
	if err != nil {
		return ProxyResult{}, err
	}
	defer resp.Body.Close()

	page, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return ProxyResult{}, err
	}

	if siteID, path, ok := parseKentaaIDs(page); ok {
		entry.siteID, entry.kentaaPath = siteID, path
		return fetchKentaa(siteID, path)
	}
	if result, ok := parseMielipotti(page); ok {
		return result, nil
	}
	if result, ok := parseNenapaiva(page); ok {
		return result, nil
	}
	if result, ok := parseSecurycast(page); ok {
		return result, nil
	}
	return ProxyResult{}, fmt.Errorf("no supported fundraiser found")
}

// fundraiserHandler serves any supported fundraiser given as ?url=<fundraiser page>.
func fundraiserHandler(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)

	// Handle preflight requests.
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	u, err := url.Parse(r.URL.Query().Get("url"))
	if err != nil || u.Scheme != "https" || u.Host == "" {
		http.Error(w, "url must be an https fundraiser page", http.StatusBadRequest)
		return
	}
	key := u.Host + u.Path

	fundraiserMutex.Lock()
	entry := fundraiserEntries[key]
	fundraiserMutex.Unlock()

	// Return cached response if valid.
	if time.Now().Before(entry.expiry) && entry.data != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write(entry.data)
		return
	}

	result, err := fetchFundraiser("https://"+key, &entry)
	if err != nil {
		log.Printf("fundraiser %s: %v", key, err)
		http.Error(w, "Could not read a supported fundraiser from that page", http.StatusBadGateway)
		return
	}

	finalData, err := json.Marshal(result)
	if err != nil {
		http.Error(w, "Error creating response JSON", http.StatusInternalServerError)
		return
	}

	// Cache the final result.
	entry.data = finalData
	entry.expiry = time.Now().Add(cacheDuration)
	fundraiserMutex.Lock()
	fundraiserEntries[key] = entry
	fundraiserMutex.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.Write(finalData)
}

// indexHandler serves the UI for building a /fundraiser URL.
func indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(indexHTML)
}

// widgetHandler serves the stream overlay widget, e.g. /widget?url=<fundraiser page>.
func widgetHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(widgetHTML)
}

func main() {
	// Read port from command line with a default value.
	port := flag.String("port", "8080", "Port to run the proxy server on")
	flag.Parse()

	http.HandleFunc("/kentaa", kentaaHandler)
	http.HandleFunc("/fundraiser", fundraiserHandler)
	http.HandleFunc("/widget", widgetHandler)
	http.HandleFunc("/", indexHandler)
	log.Printf("Proxy server is running on port %s...\n", *port)
	log.Printf("Endpoints: /kentaa, /fundraiser?url=<fundraiser page>, /widget?url=<fundraiser page>, /\n")
	log.Fatal(http.ListenAndServe(":"+*port, nil))
}
