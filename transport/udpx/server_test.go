package udpx_test

import (
	"context"
	"log"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/bang-go/micro/transport/udpx"
)

func TestServerStart(t *testing.T) {
	server := udpx.NewServer(&udpx.ServerConfig{Addr: "127.0.0.1:8083"})

	go func() {
		time.Sleep(2 * time.Second)
		server.Shutdown(context.Background())
	}()

	err := server.Start(udpx.HandlerFunc(func(ctx context.Context, packet []byte, addr net.Addr, conn *net.UDPConn) error {
		// Do not close connection in handler for UDP server, it's shared!
		// defer conn.Close()
		log.Println(time.Now().String())
		// Echo back
		go func() {
			sd := append([]byte("client: "), packet...)
			_, err := conn.WriteToUDP(sd, addr.(*net.UDPAddr))
			if err != nil {
				log.Println(err.Error())
				return
			}
			log.Println(string(packet))
		}()
		return nil
	}))
	// Start returns error on shutdown usually or nil?
	// udpx Start loop returns nil on stopCh close.
	if err != nil {
		t.Logf("Server stopped with error: %v", err)
	}
}

func TestIntegration(t *testing.T) {
	// Start Server
	server := udpx.NewServer(&udpx.ServerConfig{Addr: "127.0.0.1:8084"})
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		err := server.Start(udpx.HandlerFunc(func(ctx context.Context, packet []byte, addr net.Addr, conn *net.UDPConn) error {
			_, err := conn.WriteToUDP(packet, addr.(*net.UDPAddr))
			return err
		}))
		if err != nil {
			// t.Log(err)
		}
	}()

	// Wait for server to start
	time.Sleep(time.Second)

	// Client
	client := udpx.NewClient(&udpx.ClientConfig{Addr: "127.0.0.1:8084", Timeout: time.Second * 5})
	connect, err := client.Dial()
	if err != nil {
		t.Fatal(err)
	}
	defer connect.Close()

	msg := []byte("hello")
	err = connect.Send(msg)
	if err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 1024)
	err = connect.Receive(buf)
	if err != nil {
		t.Fatal(err)
	}

	if string(buf[:len(msg)]) != string(msg) {
		t.Fatalf("expected %s, got %s", msg, buf[:len(msg)])
	}

	server.Shutdown(context.Background())
	wg.Wait()
}
