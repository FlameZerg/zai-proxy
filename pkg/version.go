package pkg

import (
	"io"
	"net/http"
	"regexp"
	"sync"
	"time"
)

var (
	feVersion   string
	versionLock sync.RWMutex
)

func GetFeVersion() string {
	versionLock.RLock()
	v := feVersion
	versionLock.RUnlock()

	if v != "" {
		return v
	}

	// Try fetching once if empty
	fetchFeVersion()
	
	versionLock.RLock()
	v = feVersion
	versionLock.RUnlock()
	
	if v != "" {
		return v
	}
	
	// Fallback if fetch failed (hardcoded recent valid version)
	return "20241108.1" 
}

func fetchFeVersion() {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("https://chat.z.ai/")
	if err != nil {
		LogError("Failed to fetch fe version: %v", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		LogError("Failed to read fe version response: %v", err)
		return
	}

	// Pattern to match: "prod-fe-frontend-20241108.1" or similar
	// Simplified regex to catch date-like versions
	re := regexp.MustCompile(`prod-fe-[a-zA-Z0-9\.-]+`)
	match := re.FindString(string(body))
	if match != "" {
		// Clean up prefix if needed, usually we just send the whole thing
		// Example: "prod-fe-frontend-20241108.1"
		
		// If strict version required (e.g. 20241108.1), refine regex
		// But usually X-FE-Version expects the full string found in main.js URL or similar
		// Let's stick to what worked before or a safe default.
		// The error message said "Minimum required: 1.0.0". 
		// Actually, let's use a very recent one found in logs or hardcode a safe one.
		// A common format is YYYYMMDD.X
		
		versionLock.Lock()
		feVersion = match
		versionLock.Unlock()
		LogInfo("Updated fe version: %s", match)
	}
}

func StartVersionUpdater() {
	fetchFeVersion()

	ticker := time.NewTicker(1 * time.Hour)
	go func() {
		for range ticker.C {
			fetchFeVersion()
		}
	}()
}
