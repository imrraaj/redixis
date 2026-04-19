package bench

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"
	"time"
)

const redixisBaseURL = "http://localhost:8080"

type accountResponse struct {
	TenantID string `json:"tenant_id"`
	APIKey   string `json:"api_key"`
}

// newHTTPClient creates a client optimized for benchmarks with connection reuse
func newHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 100,
			MaxConnsPerHost:     100,
			IdleConnTimeout:     90 * time.Second,
			DisableKeepAlives:   false,
			DialContext: (&net.Dialer{
				Timeout:   5 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
		Timeout: 10 * time.Second,
	}
}

// setupBenchAccount creates a tenant and returns credentials
func setupBenchAccount(client *http.Client) (tenantID, apiKey string, err error) {
	resp, err := client.Post(
		redixisBaseURL+"/auth/account",
		"application/json",
		bytes.NewReader([]byte("{}")),
	)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return "", "", fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var acc accountResponse
	if err := json.NewDecoder(resp.Body).Decode(&acc); err != nil {
		return "", "", err
	}
	return acc.TenantID, acc.APIKey, nil
}

// BenchmarkRedixis_SET tests SET operations through Redixis API
func BenchmarkRedixis_SET(b *testing.B) {
	client := newHTTPClient()

	// Skip if server not running
	if _, err := client.Get(redixisBaseURL + "/healthz"); err != nil {
		b.Skip("Redixis server not available:", err)
	}

	tenantID, apiKey, err := setupBenchAccount(client)
	if err != nil {
		b.Fatalf("failed to create account: %v", err)
	}

	// Pre-allocate request body to avoid allocations in hot path
	payloadTemplate := `{"key":"key%d","value":"value","ttl_seconds":60}`

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			payload := fmt.Sprintf(payloadTemplate, i)
			req, _ := http.NewRequest(
				"POST",
				fmt.Sprintf("%s/v1/%s/SET", redixisBaseURL, tenantID),
				bytes.NewReader([]byte(payload)),
			)
			req.Header.Set("X-API-Key", apiKey)
			req.Header.Set("Content-Type", "application/json")

			resp, err := client.Do(req)
			if err != nil {
				b.Fatalf("request failed: %v", err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				b.Fatalf("unexpected status: %d", resp.StatusCode)
			}
			i++
		}
	})
}

// BenchmarkRedixis_GET tests GET operations through Redixis API
func BenchmarkRedixis_GET(b *testing.B) {
	client := newHTTPClient()

	if _, err := client.Get(redixisBaseURL + "/healthz"); err != nil {
		b.Skip("Redixis server not available:", err)
	}

	tenantID, apiKey, err := setupBenchAccount(client)
	if err != nil {
		b.Fatalf("failed to create account: %v", err)
	}

	// Pre-populate keys via API (fewer keys, slower pace)
	for i := 0; i < 100; i++ {
		payload := fmt.Sprintf(`{"key":"key%d","value":"value"}`, i)
		req, _ := http.NewRequest(
			"POST",
			fmt.Sprintf("%s/v1/%s/SET", redixisBaseURL, tenantID),
			bytes.NewReader([]byte(payload)),
		)
		req.Header.Set("X-API-Key", apiKey)
		req.Header.Set("Content-Type", "application/json")
		resp, _ := client.Do(req)
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(1 * time.Millisecond) // Slow down pre-population
	}

	payloadTemplate := `{"key":"key%d"}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		payload := fmt.Sprintf(payloadTemplate, i%100)
		req, _ := http.NewRequest(
			"POST",
			fmt.Sprintf("%s/v1/%s/GET", redixisBaseURL, tenantID),
			bytes.NewReader([]byte(payload)),
		)
		req.Header.Set("X-API-Key", apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			b.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
			b.Fatalf("unexpected status: %d", resp.StatusCode)
		}
	}
}

// BenchmarkRedixis_INCR tests INCR operations through Redixis API
func BenchmarkRedixis_INCR(b *testing.B) {
	client := newHTTPClient()

	if _, err := client.Get(redixisBaseURL + "/healthz"); err != nil {
		b.Skip("Redixis server not available:", err)
	}

	tenantID, apiKey, err := setupBenchAccount(client)
	if err != nil {
		b.Fatalf("failed to create account: %v", err)
	}

	// Pre-populate counter
	req, _ := http.NewRequest(
		"POST",
		fmt.Sprintf("%s/v1/%s/SET", redixisBaseURL, tenantID),
		bytes.NewReader([]byte(`{"key":"counter","value":"0"}`)),
	)
	req.Header.Set("X-API-Key", apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, _ := client.Do(req)
	if resp != nil {
		resp.Body.Close()
	}

	payload := []byte(`{"key":"counter"}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest(
			"POST",
			fmt.Sprintf("%s/v1/%s/INCR", redixisBaseURL, tenantID),
			bytes.NewReader(payload),
		)
		req.Header.Set("X-API-Key", apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			b.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b.Fatalf("unexpected status: %d", resp.StatusCode)
		}
	}
}

// BenchmarkRedixis_MSET tests MSET operations through Redixis API
func BenchmarkRedixis_MSET(b *testing.B) {
	client := newHTTPClient()

	if _, err := client.Get(redixisBaseURL + "/healthz"); err != nil {
		b.Skip("Redixis server not available:", err)
	}

	tenantID, apiKey, err := setupBenchAccount(client)
	if err != nil {
		b.Fatalf("failed to create account: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		items := make(map[string]string)
		for j := 0; j < 10; j++ {
			items[fmt.Sprintf("batch%d_key%d", i, j)] = fmt.Sprintf("value%d", j)
		}
		payload, _ := json.Marshal(map[string]interface{}{"items": items})
		req, _ := http.NewRequest(
			"POST",
			fmt.Sprintf("%s/v1/%s/MSET", redixisBaseURL, tenantID),
			bytes.NewReader(payload),
		)
		req.Header.Set("X-API-Key", apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			b.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b.Fatalf("unexpected status: %d", resp.StatusCode)
		}
	}
}

// TestMain ensures cleanup
func TestMain(m *testing.M) {
	code := m.Run()
	os.Exit(code)
}
