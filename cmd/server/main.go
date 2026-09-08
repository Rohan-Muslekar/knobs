package main

import (
	"context"
	"log"

	"github.com/Rohan-Muslekar/knobs/internal/app"
)

func main() {
	if err := app.Run(context.Background()); err != nil {
		log.Fatal(err)
	}
}
