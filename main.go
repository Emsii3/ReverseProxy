package main

import (
	"bufio"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

func rateLimit(next http.Handler, visitors *sync.Map) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		var timesVisited = new(atomic.Int64)
		if err != nil {
			http.Error(w, "Internal server error", 500)
			return
		}
		val, _ := visitors.LoadOrStore(ip, timesVisited)
		timesVisitedFromMap := val.(*atomic.Int64)
		currentVal := timesVisitedFromMap.Add(1)
		if currentVal > 100 {
			http.Error(w, "Too many requests", 429)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func checkHealth(next http.Handler, isAlive *atomic.Bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAlive.Load() == false {
			http.Error(w, "The server was acting as a gateway or proxy and did not receive a timely response from the upstream server.", 504)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func cacheMiddleware(next http.Handler, cache *sync.Map, cacheable map[string]bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		//check if cacheable
		if allowCache, exists := cacheable[r.URL.Path]; !exists || !allowCache {
			next.ServeHTTP(w, r)
			return
		}
		//cacheing
		key := r.Method + ":" + r.URL.String()
		if val, ok := cache.Load(key); ok == true {
			box := val.(CachedResponse)
			for k, v := range box.Headers {
				w.Header()[k] = v
			}
			w.WriteHeader(box.StatusCode)
			w.Write(box.Body)
			if time.Now().After(box.ExpiresAt) {
				cache.Delete(key)
			}
			return
		} else {
			rr := newResponseRecorder(w)
			next.ServeHTTP(rr, r)
			response := CachedResponse{
				StatusCode: rr.statusCode,
				Body:       rr.buffer.Bytes(),
				Headers:    rr.Header().Clone(),
				ExpiresAt:  time.Now().Add(time.Minute * 1),
			}
			cache.Store(key, response)
		}
	})
}

func main() {
	configPath := "config.json"
	cache := new(sync.Map)                        // cache
	backends := []string{"http://localhost:8080"} //, "http://localhost:8082", "http://	localhost:8083"}
	visitors := new(sync.Map)                     // rate limiter clearing
	var currentConfig atomic.Pointer[ProxyConfig]
	loadConfig(configPath, &currentConfig) // load config once on start
	go func() {
		fileinfo, _ := os.Stat(configPath)
		lastMod := fileinfo.ModTime()
		for {
			time.Sleep(time.Second * 5)
			config := reloadConfig(configPath, lastMod)
			if config != nil {
				currentConfig.Store(config)
				fileinfo, _ = os.Stat(configPath)
				lastMod = fileinfo.ModTime()
			}

		}
	}() // reload config every 5 seconds

	go func() {
		for {
			time.Sleep(time.Second * 3)
			visitors.Clear()
		}
	}() // rate limiter reset

	var isAlive atomic.Bool // HEALTHCHECK
	go func() {
		for {
			_, err := http.Get(backends[0] + "/test")
			isAlive.Store(err == nil)
			time.Sleep(time.Minute * 1)
		}
	}()

	go func() {
		for {
			time.Sleep(time.Minute * 10)
			cache.Range(func(k, v any) bool {
				box := v.(CachedResponse)
				if time.Now().After(box.ExpiresAt) {
					cache.Delete(k)
				}
				return true
			})
		}
	}() // cache clearing
	requestsToCache := make(map[string]bool) // check what requests should be cached
	file, err := os.Open("toBeCached.txt")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		requestsToCache[scanner.Text()] = true
	}
	file.Close()
	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	parsedURLs := make([]*url.URL, len(backends))
	for i, raw := range backends {
		parsedURLs[i], err = url.Parse(raw)
		if err != nil {
			log.Fatal(err)
		}
	}

	var backendToChoose atomic.Int64
	proxy := httputil.NewSingleHostReverseProxy(parsedURLs[0])
	originalDirector := proxy.Director
	numberOfBackends := len(backends) // caching this to improve speed
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		// adding headers
		clientAddr := req.RemoteAddr
		req.Header.Set("X-Forwarded-For", clientAddr)
		req.Header.Add("X-Forwarded-Proto", "http")
		req.Header.Set("X-Real-IP", req.RemoteAddr)

		//load balancer using round-robin
		count := backendToChoose.Add(1)
		idx := int(count % int64(numberOfBackends))
		target := parsedURLs[idx]
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
	}

	println("Magia")
	http.Handle("/", checkHealth(rateLimit(cacheMiddleware(proxy, cache, requestsToCache), visitors), &isAlive))
	http.ListenAndServe(":8081", nil)
}
