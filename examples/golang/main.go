// Reference: a Go program consuming Knobs config over the delivery API.
//
//	KNOBS_ENDPOINT=http://localhost:8080 KNOBS_API_KEY=knobs_xxx go run .
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"example.com/knobs-consumer/knobs"
)

func main() {
	endpoint := env("KNOBS_ENDPOINT", "http://localhost:8080")
	apiKey := os.Getenv("KNOBS_API_KEY")
	if apiKey == "" {
		log.Fatal("KNOBS_API_KEY is required")
	}

	client := knobs.New(endpoint, apiKey)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 1. Fetch the current snapshot once.
	snap, err := client.Snapshot(ctx)
	if err != nil {
		log.Fatalf("fetch snapshot: %v", err)
	}
	if snap == nil {
		log.Println("no values set for this environment yet")
		snap = &knobs.Snapshot{}
	}
	logConfig(snap)

	// 2. Hold the config in memory; a real app reads it on the hot path.
	//    Here we just track the latest revision for reconnects.
	current := snap.Revision

	// 3. Stream live updates, reconnecting on error with the latest revision.
	for ctx.Err() == nil {
		err := client.Stream(ctx, current, func(s knobs.Snapshot) {
			current = s.Revision
			log.Println("config changed:")
			logConfig(&s)
		})
		if ctx.Err() != nil {
			break
		}
		if err != nil {
			log.Printf("stream ended (%v); reconnecting in 2s", err)
			time.Sleep(2 * time.Second)
		}
	}
	log.Println("shutting down")
}

func logConfig(s *knobs.Snapshot) {
	if n, ok := s.Int("maxRetries"); ok {
		log.Printf("  maxRetries = %d", n)
	}
	if b, ok := s.Bool("featureX"); ok {
		log.Printf("  featureX   = %t", b)
	}
	log.Printf("  (version=%d revision=%d schemaHash=%s)", s.Version, s.Revision, s.SchemaHash)
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
