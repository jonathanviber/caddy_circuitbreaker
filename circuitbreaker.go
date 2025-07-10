// Copyright 2015 Matthew Holt and The Caddy Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Original implementation by Danny Navarro @ Ardan Labs.

package circuitbreaker

import (
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/vulcand/oxy/memmetrics"
)

func init() {
	caddy.RegisterModule(Simple{})
	httpcaddyfile.RegisterHandlerDirective("circuit_breaker", parseCaddyfileHandler)
}

// Simple implements circuit breaking functionality for
// requests within this process over a sliding time window.
type Simple struct {
	tripped  int32 // accessed atomically
	cbFactor int32
	metrics  *memmetrics.RTMetrics
	Config
}

// CaddyModule returns the Caddy module information.
func (Simple) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.circuit_breaker",
		New: func() caddy.Module { return new(Simple) },
	}
}

// Provision sets up a configured circuit breaker.
func (c *Simple) Provision(ctx caddy.Context) error {
	// Set defaults if not specified
	if c.Factor == "" {
		c.Factor = "error_ratio"
	}
	if c.MinRequests == 0 {
		c.MinRequests = 10
	}
	
	f, ok := typeCB[c.Factor]
	if !ok {
		return fmt.Errorf("invalid factor '%s', must be one of: latency, error_ratio, status_ratio", c.Factor)
	}

	if c.TripDuration == 0 {
		c.TripDuration = caddy.Duration(defaultTripDuration)
	}

	mt, err := memmetrics.NewRTMetrics()
	if err != nil {
		return fmt.Errorf("cannot create new metrics: %v", err.Error())
	}

	c.cbFactor = f
	c.metrics = mt
	c.tripped = 0

	return nil
}

// ServeHTTP implements caddyhttp.MiddlewareHandler
func (c *Simple) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	if !c.OK() {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("Service Unavailable - Circuit Breaker Open"))
		return nil
	}
	
	start := time.Now()
	rec := caddyhttp.NewResponseRecorder(w, nil, func(status int, header http.Header) bool {
		return false // Don't buffer, just record metrics
	})
	
	err := next.ServeHTTP(rec, r)
	latency := time.Since(start)
	
	// Record metrics immediately (not in background)
	status := rec.Status()
	if err != nil {
		// If there's an error, treat it as a 500
		status = 500
	}
	c.RecordMetric(status, latency)
	
	return err
}

// OK returns whether the circuit breaker is tripped or not.
func (c *Simple) OK() bool {
	return atomic.LoadInt32(&c.tripped) == 0
}

// RecordMetric records a response status code and execution time of a request.
func (c *Simple) RecordMetric(statusCode int, latency time.Duration) {
	c.metrics.Record(statusCode, latency)
	c.checkAndSet()
}

// checkAndSet checks our metrics to see if we should trip our circuit breaker.
func (c *Simple) checkAndSet() {
	// Check if we have minimum requests before evaluating
	if c.metrics.TotalCount() < int64(c.MinRequests) {
		return
	}

	var isTripped bool

	switch c.cbFactor {
	case factorErrorRatio:
		// Use both network errors and 5xx status codes
		errorRatio := c.metrics.NetworkErrorRatio()
		statusRatio := c.metrics.ResponseCodeRatio(500, 600, 0, 600)
		if errorRatio > c.Threshold || statusRatio > c.Threshold {
			isTripped = true
		}
	case factorLatency:
		hist, err := c.metrics.LatencyHistogram()
		if err != nil {
			return
		}
		// For latency, threshold should be in milliseconds
		l := hist.LatencyAtQuantile(0.95) // Use 95th percentile
		if float64(l.Nanoseconds())/float64(time.Millisecond) > c.Threshold {
			isTripped = true
		}
	case factorStatusCodeRatio:
		if c.metrics.ResponseCodeRatio(500, 600, 0, 600) > c.Threshold {
			isTripped = true
		}
	}

	if isTripped {
		atomic.StoreInt32(&c.tripped, 1)

		// Reset circuit breaker after trip duration in background
		go func() {
			time.Sleep(time.Duration(c.Config.TripDuration))
			atomic.StoreInt32(&c.tripped, 0)
			// Reset metrics when circuit closes
			c.metrics.Reset()
		}()
	}
}

// Config represents the configuration of a circuit breaker.
type Config struct {
	// Threshold defines the failure rate that triggers the circuit breaker.
	// For error_ratio and status_ratio: 0.0-1.0 (e.g., 0.5 = 50% failure rate)
	// For latency: milliseconds (e.g., 100 = 100ms response time threshold)
	Threshold    float64       `json:"threshold,omitempty"`
	
	// Factor determines what metric triggers the circuit breaker:
	// "error_ratio" - network errors and 5xx status codes (default)
	// "status_ratio" - only 5xx status codes
	// "latency" - 95th percentile response time in milliseconds
	Factor       string        `json:"factor,omitempty"`
	
	// TripDuration is how long the circuit stays open after tripping.
	// During this time, all requests return 503 Service Unavailable.
	// Format: "5s", "30s", "2m" (default: 5s)
	TripDuration caddy.Duration `json:"trip_duration,omitempty"`
	
	// MinRequests is the minimum number of requests needed before the circuit
	// breaker can trip. Prevents false positives with low traffic.
	// Must be reached within the sliding window (default: 10)
	MinRequests  int           `json:"min_requests,omitempty"`
}

const (
	factorLatency = iota + 1
	factorErrorRatio
	factorStatusCodeRatio
	defaultTripDuration = 5 * time.Second
)

// typeCB handles converting a Config Factor value to the internal circuit breaker types.
var typeCB = map[string]int32{
	"latency":      factorLatency,
	"error_ratio":  factorErrorRatio,
	"status_ratio": factorStatusCodeRatio,
}

// UnmarshalCaddyfile implements caddyfile.Unmarshaler.
func (c *Simple) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for nesting := d.Nesting(); d.NextBlock(nesting); {
		switch d.Val() {
		case "threshold":
			if !d.NextArg() {
				return d.ArgErr()
			}
			val, err := strconv.ParseFloat(d.Val(), 64)
			if err != nil {
				return d.Errf("invalid threshold value: %v", err)
			}
			c.Threshold = val
		case "factor":
			if !d.NextArg() {
				return d.ArgErr()
			}
			c.Factor = d.Val()
		case "trip_duration":
			if !d.NextArg() {
				return d.ArgErr()
			}
			dur, err := time.ParseDuration(d.Val())
			if err != nil {
				return d.Errf("invalid trip_duration: %v", err)
			}
			c.TripDuration = caddy.Duration(dur)
		case "min_requests":
			if !d.NextArg() {
				return d.ArgErr()
			}
			val, err := strconv.Atoi(d.Val())
			if err != nil {
				return d.Errf("invalid min_requests value: %v", err)
			}
			c.MinRequests = val
		default:
			return d.Errf("unrecognized subdirective: %s", d.Val())
		}
	}
	return nil
}

func parseCaddyfileHandler(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var cb Simple
	err := cb.UnmarshalCaddyfile(h.Dispenser)
	if err != nil {
		return nil, err
	}
	return &cb, nil
}

// Interface guards
var (
	_ caddy.Provisioner             = (*Simple)(nil)
	_ caddyhttp.MiddlewareHandler   = (*Simple)(nil)
	_ caddyfile.Unmarshaler         = (*Simple)(nil)
)