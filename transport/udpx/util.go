package udpx

import (
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
)

func normalizeContext(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	return context.Background()
}

func validateContext(ctx context.Context) error {
	if ctx == nil {
		return ErrContextRequired
	}
	return nil
}

func classifyPacketResult(ctx context.Context, err error, panicked bool) string {
	if panicked {
		return "panic"
	}
	if errors.Is(context.Cause(ctx), errServerClosed) {
		return "shutdown"
	}
	switch {
	case err == nil:
		return "ok"
	case isClosedConnectionError(err):
		return "closed"
	default:
		return "error"
	}
}

func isClosedConnectionError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	errText := strings.ToLower(err.Error())
	return strings.Contains(errText, "use of closed network connection")
}

func isTemporaryNetError(err error) bool {
	if err == nil {
		return false
	}

	type timeout interface {
		Timeout() bool
	}
	var timeoutErr timeout
	if errors.As(err, &timeoutErr) && timeoutErr.Timeout() {
		return true
	}

	type temporary interface {
		Temporary() bool
	}
	var temporaryErr temporary
	return errors.As(err, &temporaryErr) && temporaryErr.Temporary()
}

func splitAddr(addr net.Addr) (string, int) {
	if addr == nil {
		return "", 0
	}
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String(), 0
	}
	portNum, err := strconv.Atoi(port)
	if err != nil {
		return host, 0
	}
	return host, portNum
}
