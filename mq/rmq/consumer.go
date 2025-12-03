package rmq

import (
	"context"
	"errors"
	"time"

	rmqClient "github.com/apache/rocketmq-clients/golang/v5"
	"github.com/apache/rocketmq-clients/golang/v5/credentials"
	v2 "github.com/apache/rocketmq-clients/golang/v5/protocol/v2"
	"github.com/bang-go/util"
)

const (
	DefaultConsumerAwaitDuration           = time.Second * 5
	DefaultConsumerMaxMessageNum     int32 = 16
	DefaultConsumerInvisibleDuration       = time.Second * 20
	ConsumerMaxMessageNum            int32 = 32 //rocketmq sdk限制最大只能是32
)

type SimpleConsumer = rmqClient.SimpleConsumer
type MessageView = rmqClient.MessageView
type FilterExpression = rmqClient.FilterExpression
type MessageViewFunc func(*MessageView) bool
type ErrRpcStatus = rmqClient.ErrRpcStatus

var AsErrRpcStatus = rmqClient.AsErrRpcStatus
var NewFilterExpression = rmqClient.NewFilterExpression
var NewFilterExpressionWithType = rmqClient.NewFilterExpressionWithType
var SubAll = rmqClient.SUB_ALL

const (
	CodeMessageNotFound = v2.Code_MESSAGE_NOT_FOUND
)

type Consumer interface {
	Start() error
	Receive() ([]*MessageView, error)
	GetSimpleConsumer() SimpleConsumer
	Ack(ctx context.Context, messageView *MessageView) error
	Close() error
}

type consumerEntity struct {
	simpleConsumer SimpleConsumer
	*ConsumerConfig
}
type ConsumerConfig struct {
	Topic                   string
	Group                   string
	Endpoint                string
	AccessKey               string
	SecretKey               string
	SubscriptionExpressions map[string]*FilterExpression
	AwaitDuration           time.Duration // maximum waiting time for receive func
	MaxMessageNum           int32         // maximum number of messages received at one time
	InvisibleDuration       time.Duration // invisibleDuration should > 20s

}

func NewSimpleConsumer(conf *ConsumerConfig) (Consumer, error) {
	var err error
	consumer := &consumerEntity{ConsumerConfig: conf}
	await := util.If(conf.AwaitDuration > 0, conf.AwaitDuration, DefaultConsumerAwaitDuration)
	var subscriptionExpressions map[string]*FilterExpression
	if len(conf.SubscriptionExpressions) > 0 { //优先filter
		subscriptionExpressions = conf.SubscriptionExpressions
	} else if conf.Topic != "" { //topic
		subscriptionExpressions = map[string]*FilterExpression{conf.Topic: SubAll}
	} else {
		err = errors.New("未设置订阅topic")
		return nil, err
	}
	consumer.simpleConsumer, err = rmqClient.NewSimpleConsumer(&rmqClient.Config{
		Endpoint:      conf.Endpoint,
		ConsumerGroup: conf.Group,
		Credentials: &credentials.SessionCredentials{
			AccessKey:    conf.AccessKey,
			AccessSecret: conf.SecretKey,
		},
	},
		rmqClient.WithSimpleAwaitDuration(await),
		rmqClient.WithSimpleSubscriptionExpressions(subscriptionExpressions),
	)
	return consumer, err
}

func (c *consumerEntity) Start() error {
	return c.simpleConsumer.Start()
}

func (c *consumerEntity) Receive() ([]*MessageView, error) {
	maxMessageNum := util.If(c.MaxMessageNum > 0, c.MaxMessageNum, DefaultConsumerMaxMessageNum)
	invisibleDuration := util.If(c.InvisibleDuration > 0, c.InvisibleDuration, DefaultConsumerInvisibleDuration)
	return c.simpleConsumer.Receive(context.TODO(), maxMessageNum, invisibleDuration)
}

func (c *consumerEntity) GetSimpleConsumer() SimpleConsumer {
	return c.simpleConsumer
}
func (c *consumerEntity) Ack(ctx context.Context, messageView *MessageView) error {
	return c.simpleConsumer.Ack(ctx, messageView)
}
func (c *consumerEntity) Close() error {
	return c.simpleConsumer.GracefulStop()
}
