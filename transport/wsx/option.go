package wsx

import (
	"net/http"
	"time"

	"github.com/bang-go/micro/telemetry/logger"
	"github.com/bang-go/opt"
)

type hubOptions struct {
	channel            string
	nodeID             string
	maxRoomsPerConnect int
	commandTimeout     time.Duration
	maxConcurrentSends int
}

func WithHubChannel(channel string) opt.Option[hubOptions] {
	return opt.OptionFunc[hubOptions](func(o *hubOptions) { o.channel = channel })
}

func WithHubNodeID(nodeID string) opt.Option[hubOptions] {
	return opt.OptionFunc[hubOptions](func(o *hubOptions) { o.nodeID = nodeID })
}

func WithHubMaxRoomsPerConnect(max int) opt.Option[hubOptions] {
	return opt.OptionFunc[hubOptions](func(o *hubOptions) { o.maxRoomsPerConnect = max })
}

func WithHubCommandTimeout(timeout time.Duration) opt.Option[hubOptions] {
	return opt.OptionFunc[hubOptions](func(o *hubOptions) { o.commandTimeout = timeout })
}

func WithHubMaxConcurrentSends(max int) opt.Option[hubOptions] {
	return opt.OptionFunc[hubOptions](func(o *hubOptions) { o.maxConcurrentSends = max })
}

type EndpointConfig struct {
	Logger            *logger.Logger
	CheckOrigin       func(*http.Request) bool
	HeartbeatInterval time.Duration
	WriteTimeout      time.Duration
}
