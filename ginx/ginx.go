package ginx

import (
	"context"
	"net/http"
	"time"

	"github.com/bang-go/util"
	"github.com/gin-gonic/gin"
)

type Server interface {
	Start() error
	Use(...gin.HandlerFunc)
	Engine() *http.Server
	GinEngine() *gin.Engine
	Group(relativePath string, handlers ...gin.HandlerFunc) *gin.RouterGroup
	Shutdown() error
}

type ServerConfig struct {
	Addr  string
	Mode  string
	Trace bool
	//TraceFilter gintrace.Filter
}
type ServerEntity struct {
	*ServerConfig
	ginEngine  *gin.Engine
	httpServer *http.Server
}

func New(conf *ServerConfig) Server {
	mode := util.If(conf.Mode != "", conf.Mode, gin.ReleaseMode)
	gin.SetMode(mode)
	ginEngine := gin.New()
	ginEngine.Use(gin.Recovery())
	return &ServerEntity{
		ServerConfig: conf,
		ginEngine:    ginEngine,
	}
}

func (s *ServerEntity) GinEngine() *gin.Engine {
	return s.ginEngine
}

func (s *ServerEntity) Engine() *http.Server {
	return s.httpServer
}

func (s *ServerEntity) Use(middlewares ...gin.HandlerFunc) {
	s.ginEngine.Use(middlewares...)
}

func (s *ServerEntity) Start() (err error) {
	s.httpServer = &http.Server{
		Addr:    s.Addr,
		Handler: s.ginEngine,
	}
	err = s.httpServer.ListenAndServe()
	return
}

func (s *ServerEntity) Group(relativePath string, handlers ...gin.HandlerFunc) *gin.RouterGroup {
	return s.ginEngine.Group(relativePath, handlers...)
}

func (s *ServerEntity) Shutdown() error {
	if s.httpServer == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return s.httpServer.Shutdown(ctx)
}
