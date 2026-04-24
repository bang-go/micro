package kafka

import "github.com/bang-go/micro/telemetry/logger"

func defaultLogger(l *logger.Logger) *logger.Logger {
	if l != nil {
		return l
	}
	return logger.New()
}
