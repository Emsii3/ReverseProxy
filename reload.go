package main

import (
	"encoding/json"
	"log"
	"os"
	"sync/atomic"
	"time"
)

type ProxyConfig struct {
	LoadBalancer string          `json:"load_balancer"`
	CacheRules   map[string]bool `json:"cache_rules"`
	RateLimitMax int             `json:"rate_limit_max"`
}

func reloadConfig(filename string, lastMod time.Time) *ProxyConfig {
	info, err := os.Stat(filename)
	if err != nil {
		return nil
	}
	if info.ModTime().After(lastMod) {
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
		return config
	} else {
		return nil
	}

}

func loadConfig(filename string, config *atomic.Pointer[ProxyConfig]) {
	buff, err := os.ReadFile(filename)
	if err != nil {
		log.Fatal(err)
	}
	var configToLoad ProxyConfig
	err = json.Unmarshal(buff, &configToLoad)
	if err != nil {
		log.Fatal(err)
	}
	config.Store(&configToLoad)
}
