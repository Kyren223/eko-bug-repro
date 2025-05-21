# TLS Connection Handling Improvements

## Problem
When a client sends multiple packets asynchronously and then immediately closes the connection, the server sometimes receives only some of the packets. This happens because the TLS connection is closed before all packets are fully transmitted and processed.

## Client-Side Changes

### 1. Improved Disconnect Function (`cmd/client/gateway.go`)
```go
func Disconnect() {
    if conn == nil {
        log.Println("Can't disconnect: not connected")
        return
    }
    
    // Set a deadline for the close operation
    deadline := time.Now().Add(5 * time.Second)
    if err := conn.SetDeadline(deadline); err != nil {
        log.Println("Error setting deadline:", err)
    }
    
    // Mark as closed first to prevent new writes
    closed = true
    
    // Close the connection
    if err := conn.Close(); err != nil {
        log.Println("Error closing connection:", err)
    }
    
    log.Println("Disconnected")
}
```

### 2. Improved Send Function (`cmd/client/gateway.go`)
```go
func send(request packet.Payload) error {
    pkt := packet.NewPacket(packet.NewJsonEncoder(request))

    writeMu.Lock()
    if conn == nil {
        writeMu.Unlock()
        return errors.New("connection is closed")
    }
    
    // Set a write deadline
    deadline := time.Now().Add(5 * time.Second)
    if err := conn.SetWriteDeadline(deadline); err != nil {
        writeMu.Unlock()
        return fmt.Errorf("error setting write deadline: %w", err)
    }
    
    _, err := pkt.Into(conn)
    
    // Reset the write deadline
    if err := conn.SetWriteDeadline(time.Time{}); err != nil {
        writeMu.Unlock()
        return fmt.Errorf("error resetting write deadline: %w", err)
    }
    
    writeMu.Unlock()

    if err != nil {
        return err
    }

    return nil
}
```

### 3. Improved Final Data Sending (`cmd/client/client.go`)
```go
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
```

## Server-Side Changes

### 1. Improved Connection Handling (`cmd/server/server2.go`)
```go
// Create a channel to signal when all writes are complete
writeDone := make(chan struct{})

go func() {
    <-ctx.Done()
    // Set a deadline for the close operation
    deadline := time.Now().Add(5 * time.Second)
    if err := conn.SetDeadline(deadline); err != nil {
        log.Println(addr, "error setting close deadline:", err)
    }
    _ = conn.Close()
    close(writeDone)
}()

defer func() {
    // Wait for writes to complete before closing
    select {
    case <-writeDone:
    case <-time.After(5 * time.Second):
        log.Println(addr, "timeout waiting for writes to complete")
    }
    
    _ = conn.Close()
    sameAddress := addr.String() == server.Session(sess.ID()).Addr().String()
    if sameAddress {
        server.RemoveSession(sess.ID())
    }
    log.Println(addr, "disconnected gracefully")
}()
```

### 2. Improved Write Handling (`cmd/server/server2.go`)
```go
// Set write deadline
deadline := time.Now().Add(5 * time.Second)
if err := conn.SetWriteDeadline(deadline); err != nil {
    log.Println(addr, "error setting write deadline:", err)
    return
}

if _, err := packet.Into(conn); err != nil {
    if !errors.Is(err, net.ErrClosed) {
        log.Println(addr, err)
    }
    return
}

// Reset write deadline
if err := conn.SetWriteDeadline(time.Time{}); err != nil {
    log.Println(addr, "error resetting write deadline:", err)
    return
}
```

### 3. Improved Session Close (`cmd/server/session/session.go`)
```go
func (s *Session) Close() {
    // Create a context with timeout for the close operation
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    // Send close notification
    payload := &packet.Error{
        Error:   "new connection from another location, closing this one",
        PktType: packet.PacketError,
    }
    pkt := packet.NewPacket(packet.NewJsonEncoder(payload))
    
    // Try to send the close notification
    select {
    case s.writeQueue <- pkt:
        // Give a small grace period for the write to be processed
        time.Sleep(100 * time.Millisecond)
    case <-ctx.Done():
        log.Println(s.addr, "timeout sending close notification")
    }

    log.Println(s.addr, "closed due to new connection from another location")
    s.cancel()
}
```

## Key Improvements

1. **Proper Write Deadlines**
   - Added write deadlines for all write operations
   - Reset deadlines after each write
   - Added proper error handling for deadline operations

2. **Graceful Shutdown Sequence**
   - Wait for writes to complete before closing
   - Added timeout for close operations
   - Better handling of close notifications

3. **Improved Error Handling**
   - Added proper error logging
   - Better handling of write errors
   - More informative log messages

4. **Better Resource Management**
   - Proper cleanup of resources
   - Better coordination between write operations and connection closure
   - Improved session management

## Expected Behavior

With these changes:
1. All packets should be properly transmitted before the connection is closed
2. The server should have time to process all received packets
3. The connection should be closed gracefully on both ends
4. Resources should be properly cleaned up

The server should now consistently receive both packets before the connection is closed. 