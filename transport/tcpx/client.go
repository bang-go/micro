package tcpx

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
	conn, err := net.Dial("tcp", s.Addr)
	return NewConnect(conn, WithConnectTimeout(s.Timeout)), err
}
