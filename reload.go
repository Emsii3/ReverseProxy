package main

import (
	"encoding/json"
	"log"
	"os"
)

type ProxyConfig struct {
	Backends     []string        `json:"backends"`
	CacheRules   map[string]bool `json:"cache_rules"`
	RateLimitMax int             `json:"rate_limit_max"`
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

	if len(config.Backends) == 0 {
		log.Println("Error, config.json has no proper backends")
		return nil
	}

	return config
}
