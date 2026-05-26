package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	daytracker "github.com/aleks/daytracker"
	"github.com/aleks/daytracker/internal/api"
	"github.com/aleks/daytracker/internal/connector"
	"github.com/aleks/daytracker/internal/db"
	"github.com/aleks/daytracker/internal/worker"
)

func main() {
	database, err := db.Open()
	if err != nil {
		log.Fatalf("db: %v", err)
	}

	registry := connector.NewRegistry()
	// Connectors are registered here as they are implemented.

	w := worker.New(database, registry)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	router := api.NewRouter(database, daytracker.WebFS(), w.TriggerChan())

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: router,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		w.Run(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Printf("server: listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("server: shutting down")
	srv.Shutdown(context.Background())
	wg.Wait()
}
