package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		signal := <-signalChan
		log.Println("signal:", signal.String())
		cancel()
	}()

	server := NewServer(ctx, 7223)
	if err := server.Run(); err != nil {
		log.Println(err)
	}
}
