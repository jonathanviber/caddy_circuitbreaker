# Circuit Breaker Plugin for Caddy

A production-ready circuit breaker implementation for Caddy's reverse proxy that provides automatic failover protection and service resilience.

## Features

- **Automatic Failover**: Prevents cascading failures by temporarily stopping requests to unhealthy upstream services
- **Multiple Trip Conditions**: Configure based on error ratio, status code ratio, or response latency
- **Configurable Recovery**: Automatic recovery with customizable trip duration
- **Zero Dependencies**: Pure Go implementation with no external dependencies
- **Production Ready**: Comprehensive test coverage and battle-tested logic

## Installation

Add this plugin to your Caddy build:

```go
import _ "path/to/circuitbreaker"
```

Or use with `xcaddy`:

```bash
xcaddy build --with github.com/yourusername/caddy-circuitbreaker
```

## Configuration

### Basic Usage

```caddyfile
example.com {
    reverse_proxy upstream:8080 {
        circuit_breaker {
            threshold 0.5
            factor error_ratio
            trip_duration 30s
        }
    }
}
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `threshold` | float | 0.5 | Threshold value (0.0-1.0) for tripping the circuit |
| `factor` | string | error_ratio | Trip condition: `error_ratio`, `status_ratio`, or `latency` |
| `trip_duration` | duration | 30s | How long to keep circuit open before attempting recovery |

### Trip Factors

#### Error Ratio
Trips when the ratio of failed requests exceeds the threshold:
```caddyfile
circuit_breaker {
    threshold 0.3        # Trip at 30% error rate
    factor error_ratio
    trip_duration 60s
}
```

#### Status Ratio  
Trips when the ratio of 4xx/5xx status codes exceeds the threshold:
```caddyfile
circuit_breaker {
    threshold 0.2        # Trip at 20% error status rate
    factor status_ratio
    trip_duration 45s
}
```

#### Latency
Trips when average response time exceeds the threshold (in seconds):
```caddyfile
circuit_breaker {
    threshold 2.0        # Trip at 2 second average latency
    factor latency
    trip_duration 30s
}
```

## How It Works

The circuit breaker operates in three states:

1. **Closed** (Normal): All requests pass through to upstream
2. **Open** (Tripped): All requests immediately return 503 Service Unavailable
3. **Half-Open** (Testing): Limited requests allowed to test if upstream has recovered

### State Transitions

- **Closed → Open**: When trip condition is met (error rate, status rate, or latency exceeds threshold)
- **Open → Half-Open**: After `trip_duration` expires
- **Half-Open → Closed**: When test requests succeed
- **Half-Open → Open**: When test requests fail

## Examples

### High Availability Setup
```caddyfile
api.example.com {
    reverse_proxy backend1:8080 backend2:8080 {
        circuit_breaker {
            threshold 0.1
            factor error_ratio
            trip_duration 15s
        }
        health_uri /health
        lb_policy round_robin
    }
}
```

### Latency-Sensitive Service
```caddyfile
fast-api.example.com {
    reverse_proxy fast-service:3000 {
        circuit_breaker {
            threshold 0.5        # 500ms max average latency
            factor latency
            trip_duration 10s
        }
    }
}
```

### Microservices Gateway
```caddyfile
gateway.example.com {
    handle /users/* {
        reverse_proxy user-service:8080 {
            circuit_breaker {
                threshold 0.2
                factor status_ratio
                trip_duration 30s
            }
        }
    }
    
    handle /orders/* {
        reverse_proxy order-service:8080 {
            circuit_breaker {
                threshold 0.15
                factor error_ratio
                trip_duration 45s
            }
        }
    }
}
```

## Monitoring

The circuit breaker exposes metrics that can be monitored:

- Circuit state (closed/open/half-open)
- Trip count and reasons
- Request success/failure rates
- Average response times

Enable Caddy's metrics endpoint to access these:

```caddyfile
{
    servers {
        metrics
    }
}

:2019 {
    metrics /metrics
}
```

## Testing

Run the comprehensive test suite:

```bash
go test -v -cover
```

Run benchmarks:
```bash
go test -bench=. -benchmem
```

## License

MIT License - see LICENSE file for details.

## Contributing

1. Fork the repository
2. Create a feature branch
3. Add tests for new functionality
4. Ensure all tests pass
5. Submit a pull request

## Support

- 📖 [Caddy Documentation](https://caddyserver.com/docs/)
- 🐛 [Report Issues](https://github.com/yourusername/caddy-circuitbreaker/issues)
- 💬 [Caddy Community](https://caddy.community/)