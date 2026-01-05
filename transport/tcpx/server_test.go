package tcpx_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/bang-go/micro/transport/tcpx"
)

func TestServerStart(t *testing.T) {
	server := tcpx.NewServer(&tcpx.ServerConfig{Addr: "127.0.0.1:8082"})

	go func() {
		time.Sleep(2 * time.Second)
		server.Shutdown(context.Background())
	}()

	err := server.Start(tcpx.HandlerFunc(func(ctx context.Context, conn tcpx.Connect) error {
		defer conn.Close()
		// Single echo
		buf := make([]byte, 256)
		err := conn.Receive(buf)
		if err != nil {
			return err
		}
		sd := append([]byte("client: "), buf...)
		return conn.Send(sd)
	}))
	if err != nil {
		t.Logf("Server stopped: %v", err)
	}
}

func TestIntegration(t *testing.T) {
	server := tcpx.NewServer(&tcpx.ServerConfig{Addr: "127.0.0.1:8085"})
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		server.Start(tcpx.HandlerFunc(func(ctx context.Context, conn tcpx.Connect) error {
			defer conn.Close()
			buf := make([]byte, 1024)
			conn.Receive(buf)
			return conn.Send(buf)
		}))
	}()

	time.Sleep(time.Second)

	client := tcpx.NewClient(&tcpx.ClientConfig{Addr: "127.0.0.1:8085"})
	connect, err := client.Dial()
	if err != nil {
		t.Fatal(err)
	}
	defer connect.Close()

	msg := []byte("hello")
	connect.Send(msg)

	buf := make([]byte, 1024)
	connect.Receive(buf)

	if string(buf[:len(msg)]) != string(msg) {
		t.Fatalf("expected %s, got %s", msg, buf[:len(msg)])
	}

	server.Shutdown(context.Background())
	wg.Wait()
}
