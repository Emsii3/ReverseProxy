package main

import (
	"encoding/json"
	"log"
	"net/url"
	"os"
	"sync/atomic"
)

type ProxyConfig struct {
	Backends     []string        `json:"backends"`
	CacheRules   map[string]bool `json:"cache_rules"`
	RateLimitMax int             `json:"rate_limit_max"`
	parsedURLs   []*url.URL      `json:"-"`
}

func reloadConfig(filename string) *ProxyConfig {
	config := &ProxyConfig{}
	buff, err := os.ReadFile(filename)
	if err != nil {
		log.Println(err)
		return nil
	}
	err = json.Unmarshal(buff, config)
	if err != nil {
		log.Println(err)
		return nil
	}
	//parse urls for backends that can be used
	parsedURLs := make([]*url.URL, len(config.Backends))
	for i, raw := range config.Backends {
		parsedURLs[i], err = url.Parse(raw)
		if err != nil {
			log.Fatal(err)
		}
	}
	if len(config.Backends) == 0 {
		log.Println("Error, config.json has no proper backends")
		return nil
	}
	config.parsedURLs = parsedURLs
	return config
}