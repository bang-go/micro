package httpx

import (
	"net/http"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
)

// Config defines the configuration for both Client and Server
type Config struct {
	// Common settings
	Trace        bool
	Logger       *logger.Logger
	EnableLogger bool

	// Client specific settings
	Timeout             time.Duration
	MaxIdleConns        int
	MaxIdleConnsPerHost int
	MaxConnsPerHost     int
	IdleConnTimeout     time.Duration
	Transport           *http.Transport

	// ObservabilitySkipPaths 跳过可观测性记录（Metrics & Trace）的路径列表
	// 客户端无默认值，完全由用户配置。
	ObservabilitySkipPaths []string

	// Server specific settings
	Addr         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}
