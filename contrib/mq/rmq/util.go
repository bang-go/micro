package rmq

import (
	"context"
	"sort"
	"strings"
	"time"

	rmqClient "github.com/apache/rocketmq-clients/golang/v5"
	"github.com/bang-go/micro/telemetry/logger"
	"github.com/bang-go/util"
)

const (
	defaultStartTimeout               = 30 * time.Second
	defaultDialTimeout                = 20 * time.Second
	defaultQueryRouteTimeout          = 10 * time.Second
	defaultReceiveAwaitDuration       = 5 * time.Second
	defaultInvisibleDuration          = 20 * time.Second
	defaultReceiveMaxMessages   int32 = 16
	maxReceiveMessages          int32 = 32
)

type Message = rmqClient.Message
type SendReceipt = rmqClient.SendReceipt
type MessageView = rmqClient.MessageView
type FilterExpression = rmqClient.FilterExpression
type AsyncSendHandler func(context.Context, []*SendReceipt, error)

var (
	AsErrRpcStatus              = rmqClient.AsErrRpcStatus
	NewFilterExpression         = rmqClient.NewFilterExpression
	NewFilterExpressionWithType = rmqClient.NewFilterExpressionWithType
	SubAll                      = rmqClient.SUB_ALL
)

func defaultLogger(log *logger.Logger) *logger.Logger {
	if log != nil {
		return log
	}
	return logger.New(logger.WithLevel("info"))
}

func timeoutContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return ctx, func() {}
	}
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func cloneSubscriptions(src map[string]*FilterExpression) (map[string]*FilterExpression, error) {
	if len(src) == 0 {
		return nil, nil
	}

	cloned := make(map[string]*FilterExpression, len(src))
	seen := make(map[string]string, len(src))
	for topic, expression := range src {
		normalizedTopic := strings.TrimSpace(topic)
		if normalizedTopic == "" {
			return nil, ErrTopicRequired
		}
		if original, ok := seen[normalizedTopic]; ok && original != topic {
			return nil, ErrDuplicateSubscriptionKey
		}
		seen[normalizedTopic] = topic

		if expression == nil {
			cloned[normalizedTopic] = nil
			continue
		}
		cloned[normalizedTopic] = util.ClonePtr(expression)
	}
	return cloned, nil
}

func subscriptionsName(subscriptions map[string]*FilterExpression) string {
	if len(subscriptions) == 0 {
		return ""
	}
	topics := make([]string, 0, len(subscriptions))
	for key := range subscriptions {
		topics = append(topics, key)
	}
	sort.Strings(topics)
	return strings.Join(topics, ",")
}

func cloneMessage(message *Message) *Message {
	cloned := *message
	cloned.Topic = strings.TrimSpace(message.Topic)
	cloned.Body = append([]byte(nil), message.Body...)
	cloned.Tag = util.ClonePtr(message.Tag)
	if keys := message.GetKeys(); len(keys) > 0 {
		cloned.SetKeys(append([]string(nil), keys...)...)
	}
	return &cloned
}
