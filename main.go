package main

import (
	"log"
	"net/http"
)

func main() {
	configPath := "config.json"
	cfg := reloadConfig(configPath)
	if cfg == nil {
		log.Fatal("cannot start: invalid or missing config.json")
	}

	app := NewProxyApp(cfg)
	app.StartWorkers(configPath)

	log.Println("Initialization successful")

	if err := app.Server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("HTTP server ListenAndServe: %v", err)
	}

	<-app.IdleConnsClosed
	app.StopWorkers()
	log.Println("Shutdown successful. Quiting program")
}
