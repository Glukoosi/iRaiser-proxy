package main

import (
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

// Target API configuration variables.
var (
	targetURL         = "https://frontend-api.kentaa.nl/actions/SkoDxEDZQJRV"
	targetHeaderKey   = "x-site-id"
	targetHeaderValue = "LqS5hWxATJhq"
)

// Cache variables.
var (
	cacheData     []byte
	cacheExpiry   time.Time
	cacheMutex    sync.Mutex
	cacheDuration = 5 * time.Second
)

// UpstreamResponse represents the structure of the upstream JSON response.
type UpstreamResponse struct {
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

func proxyHandler(w http.ResponseWriter, r *http.Request) {
	// Set CORS headers to allow all origins.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	// Handle preflight requests.
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Return cached response if valid.
	cacheMutex.Lock()
	if time.Now().Before(cacheExpiry) && cacheData != nil {
		data := cacheData
		cacheMutex.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
		return
	}
	cacheMutex.Unlock()

	// Create request to target API.
	client := &http.Client{}
	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		http.Error(w, "Error creating request", http.StatusInternalServerError)
		return
	}
	req.Header.Set(targetHeaderKey, targetHeaderValue)

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
	var upstream UpstreamResponse
	if err := json.Unmarshal(body, &upstream); err != nil {
		http.Error(w, "Error parsing upstream JSON", http.StatusInternalServerError)
		return
	}

	// Prepare our proxied response with only the target and total amounts.
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
	cacheMutex.Lock()
	cacheData = finalData
	cacheExpiry = time.Now().Add(cacheDuration)
	cacheMutex.Unlock()

	// Return the filtered JSON response.
	w.Header().Set("Content-Type", "application/json")
	w.Write(finalData)
}

func main() {
	// Read port from command line with a default value.
	port := flag.String("port", "8080", "Port to run the proxy server on")
	flag.Parse()

	http.HandleFunc("/", proxyHandler)
	log.Printf("Proxy server is running on port %s...\n", *port)
	log.Fatal(http.ListenAndServe(":"+*port, nil))
}
