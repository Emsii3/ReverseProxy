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

### IP-based Rate Limiting
* Built-in, thread-safe rate limiter utilizing `sync.Map` to protect backend services from HTTP floods and basic DDoS attacks.

### In-Memory Caching
* Configurable caching middleware with TTL (Time-To-Live) expiration.
* Drastically reduces backend load by serving frequent identical requests straight from RAM.

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
  "rate_limit_max": 50
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
| **Health Check** (Active Backend) | 605.7 ns/op | 5370 B/op | 15 |
| **Rate Limiter** (Under Limit) | 640.8 ns/op | 5402 B/op | 17 |
| **Rate Limiter** (Heavy IP Rotation) | 1347.0 ns/op | 5521 B/op | 19 |
| **Cache** (Miss - Write to RAM) | 847.0 ns/op | 5946 B/op | 26 |
| **Cache** (Hit - Read from RAM) | 704.1 ns/op | 5418 B/op | 16 |
| **Cache** (Hit - Parallel Execution) | 854.7 ns/op | 5418 B/op | 16 |
| **Cache** (Non-Cacheable Path) | 631.3 ns/op | 5380 B/op | 15 |
| **Cache** (Expired Entry Cleanup) | 862.0 ns/op | 5786 B/op | 22 |
| **Full Chain** (Cache Hit) | 904.6 ns/op | 5442 B/op | 18 |
| **Full Chain** (Cache Miss) | 945.9 ns/op | 6074 B/op | 30 |
| **Hot Reload** (JSON Parsing & Swap) | 9320.0 ns/op | 1880 B/op | 22 |

*Note: The entire request lifecycle (Full Chain) executes in less than 1 microsecond per operation, proving the efficiency of the lock-free state management architecture.*
