package udpx_test

import (
	"context"
	"log"
	"net"
	"testing"
	"time"

	"github.com/bang-go/micro/transport/udpx"
)

func TestServerStart(t *testing.T) {
	server := udpx.NewServer(&udpx.ServerConfig{Addr: "127.0.0.1:8083"})
	err := server.Start(udpx.HandlerFunc(func(ctx context.Context, packet []byte, addr net.Addr, conn *net.UDPConn) error {
		defer conn.Close()
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
	if err != nil {
		log.Fatal(err)
	}
}
