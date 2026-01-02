package udpx

import (
	"net"
	"time"
)

type Client interface {
	Dial() (Connect, error)
}

type ClientConfig struct {
	Addr    string
	Timeout time.Duration
}

type clientEntity struct {
	*ClientConfig
}

func NewClient(conf *ClientConfig) Client {
	return &clientEntity{ClientConfig: conf}
}

func (s *clientEntity) Dial() (Connect, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", s.Addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialUDP("udp", nil, udpAddr)
	return NewConnect(conn, WithConnectTimeout(s.Timeout)), err
}
