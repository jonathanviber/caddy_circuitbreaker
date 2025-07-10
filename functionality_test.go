package circuitbreaker

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

// Test basic provisioning and circuit breaker functionality
func TestCircuitBreakerCore(t *testing.T) {
	cb := &Simple{
		Config: Config{
			Threshold:    0.1,
			Factor:       "error_ratio", 
			TripDuration: caddy.Duration(30 * time.Second),
			MinRequests:  5,
		},
	}

	ctx := caddy.Context{}
	err := cb.Provision(ctx)
	if err != nil {
		t.Fatalf("Failed to provision: %v", err)
	}

	// Test initial state
	if !cb.OK() {
		t.Error("Circuit breaker should start OK")
	}

	// Test metric recording
	cb.RecordMetric(200, 50*time.Millisecond)
	cb.RecordMetric(500, 50*time.Millisecond)
	
	// Circuit should still be OK with low error rate
	if !cb.OK() {
		t.Error("Circuit breaker should remain OK with low error rate")
	}
}

// Test error ratio circuit breaker tripping
func TestErrorRatioTripping(t *testing.T) {
	cb := &Simple{
		Config: Config{
			Threshold:    0.5,
			Factor:       "error_ratio",
			TripDuration: caddy.Duration(100 * time.Millisecond),
			MinRequests:  5,
		},
	}

	ctx := caddy.Context{}
	err := cb.Provision(ctx)
	if err != nil {
		t.Fatalf("Failed to provision: %v", err)
	}

	// Record enough requests to meet minimum
	for i := 0; i < 3; i++ {
		cb.RecordMetric(200, 10*time.Millisecond)
	}
	// Record high error rate
	for i := 0; i < 4; i++ {
		cb.RecordMetric(500, 10*time.Millisecond)
	}

	// Circuit should trip due to high error rate (4/7 = 0.57 > 0.5)
	if cb.OK() {
		t.Error("Circuit breaker should have tripped due to high error rate")
	}

	// Wait for circuit to reset
	time.Sleep(150 * time.Millisecond)
	if !cb.OK() {
		t.Error("Circuit breaker should have reset after trip duration")
	}
}

// Test status ratio circuit breaker tripping
func TestStatusRatioTripping(t *testing.T) {
	cb := &Simple{
		Config: Config{
			Threshold:    0.4,
			Factor:       "status_ratio",
			TripDuration: caddy.Duration(100 * time.Millisecond),
			MinRequests:  5,
		},
	}

	ctx := caddy.Context{}
	err := cb.Provision(ctx)
	if err != nil {
		t.Fatalf("Failed to provision: %v", err)
	}

	// Record requests with high 5xx ratio
	for i := 0; i < 3; i++ {
		cb.RecordMetric(200, 10*time.Millisecond)
	}
	for i := 0; i < 3; i++ {
		cb.RecordMetric(503, 10*time.Millisecond)
	}

	// Circuit should trip (3/6 = 0.5 > 0.4)
	if cb.OK() {
		t.Error("Circuit breaker should have tripped due to high status ratio")
	}
}

// Test latency circuit breaker tripping
func TestLatencyTripping(t *testing.T) {
	cb := &Simple{
		Config: Config{
			Threshold:    10, // 10ms threshold
			Factor:       "latency",
			TripDuration: caddy.Duration(100 * time.Millisecond),
			MinRequests:  5,
		},
	}

	ctx := caddy.Context{}
	err := cb.Provision(ctx)
	if err != nil {
		t.Fatalf("Failed to provision: %v", err)
	}

	// Record many requests with very high latency to ensure 95th percentile exceeds threshold
	for i := 0; i < 100; i++ {
		cb.RecordMetric(200, 500*time.Millisecond) // 500ms >> 10ms threshold
	}

	// Circuit should trip due to high latency
	if cb.OK() {
		t.Error("Circuit breaker should have tripped due to high latency")
	}
}

// Test minimum requests requirement
func TestMinimumRequestsRequirement(t *testing.T) {
	cb := &Simple{
		Config: Config{
			Threshold:    0.1,
			Factor:       "error_ratio",
			TripDuration: caddy.Duration(100 * time.Millisecond),
			MinRequests:  10,
		},
	}

	ctx := caddy.Context{}
	err := cb.Provision(ctx)
	if err != nil {
		t.Fatalf("Failed to provision: %v", err)
	}

	// Record high error rate but below minimum requests
	for i := 0; i < 5; i++ {
		cb.RecordMetric(500, 10*time.Millisecond)
	}

	// Circuit should NOT trip due to insufficient requests
	if !cb.OK() {
		t.Error("Circuit breaker should not trip with insufficient requests")
	}

	// Add more requests to meet minimum
	for i := 0; i < 5; i++ {
		cb.RecordMetric(500, 10*time.Millisecond)
	}

	// Now circuit should trip
	if cb.OK() {
		t.Error("Circuit breaker should trip after meeting minimum requests")
	}
}

// Test ServeHTTP when circuit is open
func TestServeHTTPCircuitOpen(t *testing.T) {
	cb := &Simple{
		Config: Config{
			Threshold:    0.1,
			Factor:       "error_ratio",
			TripDuration: caddy.Duration(1 * time.Second),
			MinRequests:  1,
		},
	}

	ctx := caddy.Context{}
	err := cb.Provision(ctx)
	if err != nil {
		t.Fatalf("Failed to provision: %v", err)
	}

	// Trip the circuit
	for i := 0; i < 5; i++ {
		cb.RecordMetric(500, 10*time.Millisecond)
	}

	// Create test request
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	// Mock next handler
	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.WriteHeader(http.StatusOK)
		return nil
	})

	// ServeHTTP should return 503 when circuit is open
	err = cb.ServeHTTP(w, req, next)
	if err != nil {
		t.Errorf("ServeHTTP returned error: %v", err)
	}

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

// Test ServeHTTP when circuit is closed
func TestServeHTTPCircuitClosed(t *testing.T) {
	cb := &Simple{
		Config: Config{
			Threshold:    0.9,
			Factor:       "error_ratio",
			TripDuration: caddy.Duration(1 * time.Second),
			MinRequests:  10,
		},
	}

	ctx := caddy.Context{}
	err := cb.Provision(ctx)
	if err != nil {
		t.Fatalf("Failed to provision: %v", err)
	}

	// Create test request
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	// Mock next handler
	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.WriteHeader(http.StatusOK)
		return nil
	})

	// ServeHTTP should pass through when circuit is closed
	err = cb.ServeHTTP(w, req, next)
	if err != nil {
		t.Errorf("ServeHTTP returned error: %v", err)
	}

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}
}