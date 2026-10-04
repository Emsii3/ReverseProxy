# Go Reverse Proxy & Load Balancer
*Lock-free routing, active health checks, and rate limiting written from scratch.*

This project was created for educational purposes as a deep dive into distributed systems, concurrent programming, and high-performance network infrastructure. The main goal was to reject massive, ready-to-use web frameworks and build a resilient gateway from scratch, relying exclusively on Go's standard library (`net/http`, `sync`, `sync/atomic`).

## Key Features

### Lock-free Configuration Hot-Reload
* Update routing rules, backend servers, and rate limits on the fly.
* Uses `atomic.Pointer` for state swapping, ensuring zero downtime and zero dropped requests during configuration reloads.

### Round-Robin Load Balancing
* Round-robin load balancer distributing traffic across configured backends.

### Active Asynchronous Health Checks
* A dedicated background worker constantly pings backend servers.
* Automatically removes unresponsive servers from the active routing pool and seamlessly reintroduces them once they recover.
* Drains response bodies to enable TCP connection reuse (HTTP Keep-Alive).

### IP-based Rate Limiting
* Built-in, thread-safe rate limiter utilizing `sync.Map` to protect backend services from HTTP floods and basic DDoS attacks.

### In-Memory Caching
* Configurable caching middleware with TTL (Time-To-Live) expiration.
* Restricts caching to idempotent `GET`/`HEAD` requests and `2xx` status codes with a 5 MB payload ceiling.
* Drastically reduces backend load by serving frequent identical requests straight from RAM.

### Panic Recovery Middleware
* Catches unexpected runtime panics across all handlers and middleware using `recover()` and detailed stack trace logging, preventing server crashes and returning clean `HTTP 500 Internal Server Error` responses.

### Custom Upstream Error Handling & Slowloris Defense
* Configured server timeouts (`ReadHeaderTimeout: 5s`, `ReadTimeout: 15s`, `WriteTimeout: 15s`, `IdleTimeout: 60s`) mitigating Slowloris and stalled connection attacks.
* Custom `proxy.ErrorHandler` distinguishing between upstream timeouts (`HTTP 504 Gateway Timeout`) and connection failures (`HTTP 502 Bad Gateway`).

### Graceful Shutdown & Clean Worker Lifecycle
* Listens for termination signals (`SIGINT`, `SIGTERM`) to cleanly finish in-flight requests and shut down the HTTP server without dropping active connections.
* Coordinates background workers using `context.Context` and `sync.WaitGroup`, guaranteeing zero goroutine leaks on shutdown.

## Technologies

* **Go (Golang)** - The core programming language.
* **net/http** - For low-level HTTP server and reverse proxy implementation.
* **sync / sync/atomic** - For advanced, lock-free memory management and preventing race conditions in a highly concurrent environment.

## Getting Started

To compile and run the project, you need to have Go installed on your machine. All dependencies are part of the standard library, so no external downloads are required.

### Configuration

The proxy requires a `config.json` file in the root directory. This file is monitored for hot-reloading, meaning you can update it while the server is running without dropping connections.

```json
{
  "backends": [
    "http://localhost:8080"
  ],
  "cache_rules": {
    "/": true,
    "/test": true
  },
  "rate_limit_max": 50,
  "max_conns_per_host": 150,
  "max_idle_conns_per_host": 150,
  "max_client_conns": 450
}
```

### Compilation

Run the following command in the project's root directory:

```bash
go build -o reverseproxy .
```

## Performance & Benchmarks

The system is designed to avoid heavy mutex locks in favor of atomic operations, allowing it to handle massive concurrency with sub-millisecond routing latency. 

Below are the benchmark results executed on an **Apple M5 (ARM64)** processor:

| Component / Scenario | Time per Operation | Memory Allocated | Allocs / Op |
| :--- | :--- | :--- | :--- |
| **Health Check** (Active Backend) | 614.4 ns/op | 5370 B/op | 15 |
| **Rate Limiter** (Under Limit) | 651.1 ns/op | 5402 B/op | 17 |
| **Rate Limiter** (Heavy IP Rotation) | 1363.0 ns/op | 5521 B/op | 19 |
| **Cache** (Miss - Write to RAM) | 859.2 ns/op | 5946 B/op | 26 |
| **Cache** (Hit - Read from RAM) | 714.2 ns/op | 5418 B/op | 16 |
| **Cache** (Hit - Parallel Execution) | 823.8 ns/op | 5418 B/op | 16 |
| **Cache** (Non-Cacheable Path) | 638.3 ns/op | 5380 B/op | 15 |
| **Cache** (Expired Entry Cleanup) | 858.9 ns/op | 5786 B/op | 22 |
| **Full Chain** (Cache Hit) | 862.6 ns/op | 5442 B/op | 18 |
| **Full Chain** (Cache Miss) | 941.0 ns/op | 6074 B/op | 30 |
| **Hot Reload** (JSON Parsing & Swap) | 9793.0 ns/op | 1576 B/op | 19 |

*Note: The entire request lifecycle (Full Chain) executes in less than 1 microsecond per operation, proving the efficiency of the lock-free state management architecture.*

## Testing & Reliability

The codebase features comprehensive unit, middleware, and end-to-end integration tests with **87.4% statement coverage**, verified against race conditions with Go's race detector:

```bash
go test -v -race ./...
```

## Future Improvements (Roadmap v2.0)

Planned architectural enhancements for enterprise-scale deployments:

* **Configurable Health Check Path & Method:** Supporting dynamic endpoints (e.g., `health_check_path: "/healthz"` or lightweight `HEAD /`) configured per backend instead of the fixed `/test` route.
* **Zero-Allocation Buffer Pooling (`sync.Pool`):** Reusing `bytes.Buffer` instances to eliminate allocation overhead during response capture.
* **Streaming & Backpressure:** Forwarding large chunked responses in real time without buffering entire payloads in RAM.
* **Prometheus Metrics & Observability:** Exposing a `/metrics` endpoint for latency percentiles (p50/p95/p99), error rates, and cache hit ratios.
* **LRU Cache Eviction Policy:** Evicting stale keys based on memory limits rather than relying solely on TTL.
* **TLS / HTTPS Termination:** Native SSL/TLS handling and automatic ACME/Let's Encrypt certificate renewal.
