package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"
)

// Kentaa API configuration.
var (
	kentaaURL         = "https://frontend-api.kentaa.nl/actions/DHHj8a7itW5G"
	kentaaHeaderKey   = "x-site-id"
	kentaaHeaderValue = "o9vDUSYsMfDp"
)

// Securycast page configuration (scraping SSR page instead of delayed API).
var (
	securycastURL = "https://oma.kummit.fi/keräys/nurmikkotv-2026"
)

// Cache variables for Kentaa.
var (
	kentaaCacheData   []byte
	kentaaCacheExpiry time.Time
	kentaaCacheMutex  sync.Mutex
)

// Cache variables for Securycast.
var (
	securycastCacheData   []byte
	securycastCacheExpiry time.Time
	securycastCacheMutex  sync.Mutex
)

var cacheDuration = 5 * time.Second

// KentaaResponse represents the structure of the Kentaa upstream JSON response.
type KentaaResponse struct {
	Data struct {
		TargetAmount int    `json:"target_amount"`
		TotalAmount  string `json:"total_amount"`
	} `json:"data"`
}

// Regex to parse donation amount from SSR page (matches "X € kerätty" pattern).
// Handles thousand separators (spaces, non-breaking spaces, and &#xA0; HTML entities),
// and the HTML entity &euro; as well as the literal € character.
var donationRegex = regexp.MustCompile(`([\d\s\x{00A0}]+(?:&#xA0;[\d]+)*)[\s\x{00A0}]*(?:€|&euro;)[\s\x{00A0}]*kerätty`)
var spaceStripRegex = regexp.MustCompile(`[\s\x{00A0}]+|&#xA0;`)

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

func securycastHandler(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)

	// Handle preflight requests.
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Return cached response if valid.
	securycastCacheMutex.Lock()
	if time.Now().Before(securycastCacheExpiry) && securycastCacheData != nil {
		data := securycastCacheData
		securycastCacheMutex.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
		return
	}
	securycastCacheMutex.Unlock()

	// Create request to Securycast API.
	client := &http.Client{}
	req, err := http.NewRequest("GET", securycastURL, nil)
	if err != nil {
		http.Error(w, "Error creating request", http.StatusInternalServerError)
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "Error fetching remote data", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Read the upstream HTML response.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "Error reading response", http.StatusInternalServerError)
		return
	}

	// Parse the donation amount from the SSR HTML page.
	matches := donationRegex.FindSubmatch(body)
	if matches == nil || len(matches) < 2 {
		http.Error(w, "Error parsing donation amount from page", http.StatusInternalServerError)
		return
	}

	// Remove spaces and non-breaking spaces from the amount string.
	amountStr := spaceStripRegex.ReplaceAllString(string(matches[1]), "")

	amount, err := strconv.Atoi(amountStr)
	if err != nil {
		http.Error(w, "Error converting donation amount", http.StatusInternalServerError)
		return
	}

	// Prepare our proxied response.
	proxyResult := ProxyResult{
		TargetAmount: 0, // No goal shown on page
		TotalAmount:  fmt.Sprintf("%d", amount),
	}

	finalData, err := json.Marshal(proxyResult)
	if err != nil {
		http.Error(w, "Error creating response JSON", http.StatusInternalServerError)
		return
	}

	// Cache the final result.
	securycastCacheMutex.Lock()
	securycastCacheData = finalData
	securycastCacheExpiry = time.Now().Add(cacheDuration)
	securycastCacheMutex.Unlock()

	// Return the filtered JSON response.
	w.Header().Set("Content-Type", "application/json")
	w.Write(finalData)
}

func main() {
	// Read port from command line with a default value.
	port := flag.String("port", "8080", "Port to run the proxy server on")
	flag.Parse()

	http.HandleFunc("/kentaa", kentaaHandler)
	http.HandleFunc("/", securycastHandler)
	log.Printf("Proxy server is running on port %s...\n", *port)
	log.Printf("Endpoints: /kentaa, /\n")
	log.Fatal(http.ListenAndServe(":"+*port, nil))
}
