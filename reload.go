package main

import (
	"encoding/json"
	"log"
	"os"
)

type ProxyConfig struct {
	Backends            []string        `json:"backends"`
	CacheRules          map[string]bool `json:"cache_rules"`
	RateLimitMax        int             `json:"rate_limit_max"`
	MaxConnsPerHost     int             `json:"max_conns_per_host"`
	MaxIdleConnsPerHost int             `json:"max_idle_conns_per_host"`
	MaxClientConns      int             `json:"max_client_conns"`
}

func reloadConfig(filename string) *ProxyConfig {
	config := &ProxyConfig{}
	buff, err := os.ReadFile(filename)
	if err != nil {
		log.Println("Error opening config file")
		log.Println(err)
		return nil
	}

	err = json.Unmarshal(buff, config)
	if err != nil {
		log.Println("Error parsing config file")
		log.Println(err)
		return nil
	}

	if len(config.Backends) == 0 {
		log.Println("Error, config.json has no proper backends")
		return nil
	}

	if config.MaxClientConns <= 0 {
		config.MaxClientConns = 100
	}

	if config.MaxConnsPerHost <= 0 {
		config.MaxConnsPerHost = 100
	}

	if config.MaxIdleConnsPerHost <= 0 {
		config.MaxIdleConnsPerHost = 500
	}

	return config
}
