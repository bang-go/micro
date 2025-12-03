package tcpx_test

import (
	"log"
	"testing"
	"time"

	"github.com/bang-go/micro/tcpx"
)

func TestServerStart(t *testing.T) {
	server := tcpx.NewServer(&tcpx.ServerConfig{Addr: "127.0.0.1:8082"})
	err := server.Start(func(conn tcpx.Connect) {
		defer conn.Close()
		for {
			log.Println(time.Now().String())
			buf := make([]byte, 256)
			err := conn.Receive(buf)
			if err != nil {
				log.Println(err.Error())
				return
			}
			sd := append([]byte("client: "), buf...)
			err = conn.Send(sd)
			if err != nil {
				log.Println(err.Error())
				return
			}
			log.Println(string(buf))
			time.Sleep(2 * time.Second)
		}
	})
	if err != nil {
		log.Fatal(err)
	}
}
