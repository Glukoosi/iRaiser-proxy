package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
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

// fundraiserEntry holds the resolved Kentaa IDs of a fundraiser page and its cached result.
type fundraiserEntry struct {
	siteID, actionID string
	data             []byte
	expiry           time.Time
}

// Fundraiser entries keyed by page host+path.
// ponytail: unbounded map, add eviction if it grows too big.
var (
	fundraiserEntries = map[string]fundraiserEntry{}
	fundraiserMutex   sync.Mutex
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

// Kentaa pages expose their IDs as data attributes on the <body> tag.
var bodyTagRegex = regexp.MustCompile(`<body[^>]*>`)
var kentaaAttrRegex = regexp.MustCompile(`data-(site-id|action-id)="([A-Za-z0-9]+)"`)

//go:embed index.html
var indexHTML []byte

// parseKentaaIDs extracts the site and action IDs from a Kentaa fundraiser page.
func parseKentaaIDs(page []byte) (siteID, actionID string, ok bool) {
	for _, m := range kentaaAttrRegex.FindAllSubmatch(bodyTagRegex.Find(page), -1) {
		if string(m[1]) == "site-id" {
			siteID = string(m[2])
		} else {
			actionID = string(m[2])
		}
	}
	return siteID, actionID, siteID != "" && actionID != ""
}

// resolveFundraiserPage fetches a fundraiser page and returns an entry with its IDs.
func resolveFundraiserPage(pageURL string) (fundraiserEntry, error) {
	resp, err := httpClient.Get(pageURL)
	if err != nil {
		return fundraiserEntry{}, err
	}
	defer resp.Body.Close()

	page, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return fundraiserEntry{}, err
	}

	siteID, actionID, ok := parseKentaaIDs(page)
	if !ok {
		return fundraiserEntry{}, fmt.Errorf("no Kentaa IDs found")
	}
	return fundraiserEntry{siteID: siteID, actionID: actionID}, nil
}

// fundraiserHandler serves any Kentaa fundraiser given as ?url=<fundraiser page>.
func fundraiserHandler(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)

	// Handle preflight requests.
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	u, err := url.Parse(r.URL.Query().Get("url"))
	if err != nil || u.Scheme != "https" || u.Host == "" {
		http.Error(w, "url must be an https Kentaa fundraiser page", http.StatusBadRequest)
		return
	}
	key := u.Host + u.Path

	fundraiserMutex.Lock()
	entry, known := fundraiserEntries[key]
	fundraiserMutex.Unlock()

	// Return cached response if valid.
	if time.Now().Before(entry.expiry) && entry.data != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write(entry.data)
		return
	}

	// Resolve IDs from the page the first time a URL is seen.
	if !known {
		entry, err = resolveFundraiserPage("https://" + key)
		if err != nil {
			http.Error(w, "Error reading Kentaa IDs from page", http.StatusBadGateway)
			return
		}
	}

	// Create request to Kentaa API.
	req, err := http.NewRequest("GET", "https://frontend-api.kentaa.nl/actions/"+entry.actionID, nil)
	if err != nil {
		http.Error(w, "Error creating request", http.StatusInternalServerError)
		return
	}
	req.Header.Set(kentaaHeaderKey, entry.siteID)

	resp, err := httpClient.Do(req)
	if err != nil {
		http.Error(w, "Error fetching remote data", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("Kentaa API returned %d", resp.StatusCode), http.StatusBadGateway)
		return
	}

	// Parse the upstream JSON.
	var upstream KentaaResponse
	if err := json.NewDecoder(resp.Body).Decode(&upstream); err != nil {
		http.Error(w, "Error parsing upstream JSON", http.StatusInternalServerError)
		return
	}

	finalData, err := json.Marshal(ProxyResult{
		TargetAmount: upstream.Data.TargetAmount,
		TotalAmount:  upstream.Data.TotalAmount,
	})
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

func main() {
	// Read port from command line with a default value.
	port := flag.String("port", "8080", "Port to run the proxy server on")
	flag.Parse()

	http.HandleFunc("/kentaa", kentaaHandler)
	http.HandleFunc("/fundraiser", fundraiserHandler)
	http.HandleFunc("/", indexHandler)
	log.Printf("Proxy server is running on port %s...\n", *port)
	log.Printf("Endpoints: /kentaa, /fundraiser?url=<fundraiser page>, /\n")
	log.Fatal(http.ListenAndServe(":"+*port, nil))
}
