package udpx

import (
	"fmt"
	"net"
	"sync"
	"time"
)

type Server interface {
	Start(ServerRegisterFunc) error
	Shutdown() error
}
type ServerConfig struct {
	Addr    string
	Timeout time.Duration
}
type ServerRegisterFunc func(Connect)

type serverEntity struct {
	*ServerConfig
	conn      *net.UDPConn
	stopCh    chan struct{}
	isRunning bool
	mu        sync.Mutex
}

func NewServer(conf *ServerConfig) Server {
	return &serverEntity{
		ServerConfig: conf,
		stopCh:       make(chan struct{}),
		isRunning:    false,
	}
}

func (s *serverEntity) Start(register ServerRegisterFunc) (err error) {
	s.mu.Lock()
	if s.isRunning {
		s.mu.Unlock()
		return fmt.Errorf("server is already running")
	}
	s.mu.Unlock()

	udpAddr, err := net.ResolveUDPAddr("udp", s.Addr)
	if err != nil {
		return
	}
	s.conn, err = net.ListenUDP("udp", udpAddr)
	if err != nil {
		return
	}

	s.mu.Lock()
	s.isRunning = true
	stopCh := s.stopCh
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.isRunning = false
		s.mu.Unlock()
	}()

	// UDP 是无连接的，注册连接处理函数
	connect := NewConnect(s.conn, WithConnectTimeout(s.Timeout))
	register(connect)

	// 等待停止信号
	<-stopCh
	return nil
}

func (s *serverEntity) Shutdown() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isRunning && s.stopCh != nil {
		close(s.stopCh)
		s.isRunning = false
		s.stopCh = make(chan struct{}) // 重新创建，支持重启
	}
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
	return nil
}
