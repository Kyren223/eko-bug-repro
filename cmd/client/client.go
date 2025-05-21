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

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	select {
	case <-ctx.Done():
		log.Println("CTX1 DONE")
	case <-ch1:
		log.Println("CH1 DONE")
	}

	select {
	case <-ctx.Done():
		log.Println("CTX2 DONE")
	case <-ch2:
		log.Println("CH2 DONE")
	}

	// log.Println("BLOCKING...")
	// <-ctx.Done()
	// log.Println("CTX DANZO")
}
