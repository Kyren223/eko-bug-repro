package main

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"eko-bug-repro/internal/packet"
	"eko-bug-repro/pkg/assert"
	"eko-bug-repro/pkg/snowflake"
	"encoding/binary"
	"errors"
	"log"
	"net"
	"sync"
	"time"
)

var (
	framer  packet.PacketFramer
	conn    net.Conn
	writeMu sync.Mutex
	closed  = false
)

type (
	ConnectionEstablished snowflake.ID
	ConnectionFailed      error
	ConnectionLost        error
	ConnectionClosed      struct{}
)

func Connect(privKey ed25519.PrivateKey, timeout time.Duration) any {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	id, err := connect(ctx, privKey)
	if err != nil {
		return ConnectionFailed(err)
	}
	return ConnectionEstablished(id)
}

func connect(ctx context.Context, privKey ed25519.PrivateKey) (snowflake.ID, error) {
	assert.Assert(conn == nil, "cannot connect, connection is active")
	closed = false

	var id snowflake.ID
	connChan := make(chan net.Conn, 1)
	errChan := make(chan error, 1)
	go func() {
		framer = packet.NewFramer()

		certPool := x509.NewCertPool()
		// if !certPool.AppendCertsFromPEM(certs.CertPEM) {
		// 	log.Fatalln("failed to append server certificate")
		// }

		tlsConfig := &tls.Config{
			RootCAs:    certPool,
			ServerName: "eko.kyren.codes",
			MinVersion: tls.VersionTLS12,
			// This is fine, it's always false by default
			// The user may change the config, the name should be clear enough
			// that this is insecure (valid use cases are for testing purposes)
			InsecureSkipVerify: true,
		}

		address := "eko.kyren.codes"
		if true {
			address = "localhost"
		}

		connection, err := tls.Dial("tcp4", address+":7223", tlsConfig)
		if err != nil {
			errChan <- err
			return
		}
		log.Println("established connection with the server")

		if id, err = handleAuth(ctx, connection, privKey); err != nil {
			errChan <- err
			return
		}
		log.Println("successfully authenticated with the server")
		connChan <- connection
	}()

	select {
	case connection := <-connChan:
		conn = connection
	case err := <-errChan:
		return 0, err
	case <-ctx.Done():
		return 0, ctx.Err()
	}

	go readForever(conn)
	go handlePacketStream()

	return id, nil
}

func handleAuth(ctx context.Context, conn net.Conn, privKey ed25519.PrivateKey) (snowflake.ID, error) {
	const nonceSize = 32
	const packetSize = 1 + nonceSize // For version byte
	challengeRequest := make([]byte, packetSize)

	deadline, _ := ctx.Deadline()
	err := conn.SetDeadline(deadline)
	assert.NoError(err, "setting deadline should not error")
	defer func() {
		err := conn.SetDeadline(time.Time{})
		assert.NoError(err, "unsetting deadline should not error")
	}()

	bytesRead := 0
	for bytesRead < packetSize {
		n, err := conn.Read(challengeRequest[bytesRead:])
		if err != nil {
			return 0, err
		}
		bytesRead += n
	}

	assert.Assert(challengeRequest[0] == packet.VERSION, "client should always have the same version as the server")

	challengeResponse := make([]byte, 1+ed25519.PublicKeySize+ed25519.SignatureSize)
	challengeResponse[0] = packet.VERSION
	copy(challengeResponse[1:1+ed25519.PublicKeySize], privKey.Public().(ed25519.PublicKey))
	signedNonce := ed25519.Sign(privKey, challengeRequest[1:])
	n := copy(challengeResponse[1+ed25519.PublicKeySize:], signedNonce)
	assert.Assert(n == ed25519.SignatureSize, "copy should've copied the entire signature exactly")

	_, err = conn.Write(challengeResponse)
	if err != nil {
		return 0, err
	}

	var idBytes [8]byte
	bytesRead = 0
	for bytesRead < 8 {
		n, err := conn.Read(idBytes[:])
		if err != nil {
			return 0, err
		}
		bytesRead += n
	}
	id := snowflake.ID(binary.BigEndian.Uint64(idBytes[:])) // #nosec G115

	return id, nil
}

func readForever(conn net.Conn) {
	buffer := make([]byte, 512)
	for {
		n, err := conn.Read(buffer)
		if err != nil {
			onDisconnect(err)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err = framer.Push(ctx, buffer[:n])
		if ctx.Err() != nil {
			cancel()
			onDisconnect(errors.New("new packet blocked for more than a second, closing connection"))
			return
		}
		cancel()
		assert.NoError(err, "packets from server should always be correctly formatted")
	}
}

func handlePacketStream() {
	for {
		pkt, ok := <-framer.Out
		if !ok {
			break
		}

		payload, err := pkt.DecodedPayload()
		assert.NoError(err, "server should always provide a decodeable packet")

		log.Println(payload)
	}
}

func Disconnect() {
	if conn != nil {
		closed = true
		_ = conn.Close()
		log.Println("Disconnected")
	} else {
		log.Println("Can't disconnect: not connected")
	}
}

func onDisconnect(err error) {
	writeMu.Lock()
	defer writeMu.Unlock()
	if conn == nil {
		return
	}
	_ = conn.Close()
	conn = nil
	close(framer.Out)
	if closed {
		log.Println("connection closed")
		// ui.Program.Send(ConnectionClosed{})
	} else {
		log.Println("connection lost:", err)
		// ui.Program.Send(ConnectionLost(err))
	}
}

type RequestSentMsg struct {
	request packet.Payload
	err     error
}

func Send(request packet.Payload) RequestSentMsg {
	err := send(request)
	if err != nil {
		log.Println("request error:", err)
	} else {
		log.Println(request)
	}
	return RequestSentMsg{
		request: request,
		err:     err,
	}
}

func SendAsync(request packet.Payload) <-chan error {
	ch := make(chan error, 1)
	go func() {
		err := send(request)
		if err != nil {
			log.Println("async request send error:", err)
		}
		ch <- err
	}()
	return ch
}

func send(request packet.Payload) error {
	pkt := packet.NewPacket(packet.NewJsonEncoder(request))

	writeMu.Lock()
	if conn == nil {
		writeMu.Unlock()
		return errors.New("connection is closed")
	}
	_, err := pkt.Into(conn)
	writeMu.Unlock()

	if err != nil {
		return err
	}

	return nil
}
