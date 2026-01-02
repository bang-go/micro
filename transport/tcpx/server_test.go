package tcpx_test

import (
	"context"
	"log"
	"testing"
	"time"

	"github.com/bang-go/micro/transport/tcpx"
)

func TestServerStart(t *testing.T) {
	server := tcpx.NewServer(&tcpx.ServerConfig{Addr: "127.0.0.1:8082"})
	err := server.Start(tcpx.HandlerFunc(func(ctx context.Context, conn tcpx.Connect) error {
		defer conn.Close()
		for {
			log.Println(time.Now().String())
			buf := make([]byte, 256)
			err := conn.Receive(buf)
			if err != nil {
				log.Println(err.Error())
				return err
			}
			sd := append([]byte("client: "), buf...)
			err = conn.Send(sd)
			if err != nil {
				log.Println(err.Error())
				return err
			}
			log.Println(string(buf))
			time.Sleep(2 * time.Second)
		}
	}))
	if err != nil {
		log.Fatal(err)
	}
}
