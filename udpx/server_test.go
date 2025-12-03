package udpx_test

import (
	"log"
	"net"
	"testing"
	"time"

	"github.com/bang-go/micro/udpx"
)

func TestServerStart(t *testing.T) {
	server := udpx.NewServer(&udpx.ServerConfig{Addr: "127.0.0.1:8083", Timeout: 5 * time.Second})
	err := server.Start(func(conn udpx.Connect) {
		defer conn.Close()
		for {
			log.Println(time.Now().String())
			buf := make([]byte, 256)
			udpAddr, err := conn.ReceiveFromUdp(buf)
			if err != nil {
				log.Println(err.Error())
				return
			}
			go func(udpAddr *net.UDPAddr) {
				sd := append([]byte("client: "), buf...)
				err = conn.SendToUdp(sd, udpAddr)
				if err != nil {
					log.Println(err.Error())
					return
				}
				log.Println(string(buf))
				time.Sleep(2 * time.Second)
			}(udpAddr)
		}
	})
	if err != nil {
		log.Fatal(err)
	}
}
