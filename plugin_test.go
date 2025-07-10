package circuitbreaker

import (
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
)

// Test circuit_breaker directive registration
func TestCircuitBreakerDirectiveRegistration(t *testing.T) {
	caddyfileConfig := `{
		threshold 0.1
		factor error_ratio
		trip_duration 30s
		min_requests 5
	}`

	d := caddyfile.NewTestDispenser(caddyfileConfig)
	h := httpcaddyfile.Helper{Dispenser: d}
	
	cb, err := parseCaddyfileHandler(h)
	if err != nil {
		t.Fatalf("circuit_breaker directive not properly registered: %v", err)
	}
	
	if cb == nil {
		t.Fatal("Expected circuit breaker handler, got nil")
	}
	
	simpleCB := cb.(*Simple)
	if simpleCB.Threshold != 0.1 || simpleCB.Factor != "error_ratio" || simpleCB.TripDuration != caddy.Duration(30*time.Second) || simpleCB.MinRequests != 5 {
		t.Error("Configuration not parsed correctly")
	}
}

// Test module registration
func TestModuleRegistration(t *testing.T) {
	cb := Simple{}
	moduleInfo := cb.CaddyModule()
	
	expectedID := "http.handlers.circuit_breaker"
	if string(moduleInfo.ID) != expectedID {
		t.Errorf("Expected module ID '%s', got '%s'", expectedID, string(moduleInfo.ID))
	}
	
	if moduleInfo.New == nil {
		t.Error("Module constructor should not be nil")
	}
}

// Test UnmarshalCaddyfile
func TestUnmarshalCaddyfile(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected Config
	}{
		{
			name: "complete config",
			input: `{
				threshold 0.25
				factor status_ratio
				trip_duration 45s
				min_requests 15
			}`,
			expected: Config{
				Threshold:    0.25,
				Factor:       "status_ratio",
				TripDuration: caddy.Duration(45 * time.Second),
				MinRequests:  15,
			},
		},
		{
			name: "minimal config",
			input: `{
				threshold 0.8
				factor latency
			}`,
			expected: Config{
				Threshold: 0.8,
				Factor:    "latency",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := caddyfile.NewTestDispenser(tt.input)
			cb := &Simple{}
			
			err := cb.UnmarshalCaddyfile(d)
			if err != nil {
				t.Fatalf("UnmarshalCaddyfile failed: %v", err)
			}
			
			if cb.Threshold != tt.expected.Threshold {
				t.Errorf("Threshold: expected %f, got %f", tt.expected.Threshold, cb.Threshold)
			}
			if cb.Factor != tt.expected.Factor {
				t.Errorf("Factor: expected %s, got %s", tt.expected.Factor, cb.Factor)
			}
			if cb.TripDuration != tt.expected.TripDuration {
				t.Errorf("TripDuration: expected %v, got %v", tt.expected.TripDuration, cb.TripDuration)
			}
			if cb.MinRequests != tt.expected.MinRequests {
				t.Errorf("MinRequests: expected %d, got %d", tt.expected.MinRequests, cb.MinRequests)
			}
		})
	}
}

// Test invalid configuration
func TestInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{
			name: "invalid factor",
			config: Config{
				Threshold:    0.5,
				Factor:       "invalid_factor",
				TripDuration: caddy.Duration(5 * time.Second),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := &Simple{Config: tt.config}
			ctx := caddy.Context{}
			err := cb.Provision(ctx)
			
			if err == nil {
				t.Error("Expected error for invalid factor")
			}
		})
	}
}

// Test default values
func TestDefaultValues(t *testing.T) {
	cb := &Simple{Config: Config{}}
	ctx := caddy.Context{}
	err := cb.Provision(ctx)
	if err != nil {
		t.Fatalf("Expected no error for empty config (should use defaults), got: %v", err)
	}
	
	if cb.Factor != "error_ratio" {
		t.Errorf("Expected default factor 'error_ratio', got '%s'", cb.Factor)
	}
	if cb.MinRequests != 10 {
		t.Errorf("Expected default min_requests 10, got %d", cb.MinRequests)
	}
	if cb.TripDuration != caddy.Duration(defaultTripDuration) {
		t.Errorf("Expected default trip_duration %v, got %v", defaultTripDuration, cb.TripDuration)
	}
}