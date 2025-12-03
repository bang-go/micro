package tcpx

import (
	"fmt"
	"net"
	"sync"
	"time"
)

type Server interface {
	Start(ServerRegisterFunc) error
	Shutdown()
}

type ServerConfig struct {
	Addr    string
	Timeout time.Duration
}

type ServerRegisterFunc func(Connect)
type serverEntity struct {
	*ServerConfig
	listen    *net.TCPListener
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

	tcpAddr, err := net.ResolveTCPAddr("tcp", s.Addr)
	if err != nil {
		return
	}
	s.listen, err = net.ListenTCP("tcp", tcpAddr)
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

	for {
		select {
		case <-stopCh:
			return nil
		default:
			// 设置 Accept 超时，以便能够响应 stopCh
			if err := s.listen.SetDeadline(time.Now().Add(1 * time.Second)); err != nil {
				return err
			}
			conn, err1 := s.listen.Accept()
			if err1 != nil {
				if netErr, ok := err1.(net.Error); ok && netErr.Timeout() {
					continue
				}
				// 其他错误，记录但继续运行
				fmt.Println(err1)
				continue
			}
			go register(NewConnect(conn, WithConnectTimeout(s.Timeout)))
		}
	}
}

func (s *serverEntity) Shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isRunning && s.stopCh != nil {
		close(s.stopCh)
		s.isRunning = false
		s.stopCh = make(chan struct{}) // 重新创建，支持重启
	}
	if s.listen != nil {
		_ = s.listen.Close()
		s.listen = nil
	}
}
