package main

import (
	"context"
	"crypto/ed25519"
	"eko-bug-repro/internal/packet"
	"log"
	"time"
)

func main() {
	_, priv, _ := ed25519.GenerateKey(nil)

	log.SetFlags(log.Lmicroseconds)

	c := Connect(priv, time.Second)
	log.Println(c)

	Send(Payload("Yo"))

	time.Sleep(time.Second)

	sendFinalData()

	Disconnect()
}

func Payload(e string) packet.Payload {
	return &packet.Error{
		Error:   e,
		PktType: packet.PacketError,
	}
}

func sendFinalData() {
	ch1 := SendAsync(Payload("ONE"))
	ch2 := SendAsync(Payload("TWO"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Wait for both writes to complete or timeout
	done := make(chan struct{})
	go func() {
		<-ch1
		<-ch2
		close(done)
	}()

	select {
	case <-ctx.Done():
		log.Println("Timeout waiting for writes to complete")
	case <-done:
		log.Println("All writes completed successfully")
	}

	// Give a small grace period for the writes to be processed
	time.Sleep(100 * time.Millisecond)
}
