package rmq

import (
	"context"
	rmqClient "github.com/apache/rocketmq-clients/golang/v5"
	"github.com/apache/rocketmq-clients/golang/v5/credentials"
	"github.com/bang-go/util"
	"time"
)

const (
	DefaultConsumerAwaitDuration           = time.Second * 5
	DefaultConsumerMaxMessageNum     int32 = 16
	DefaultConsumerInvisibleDuration       = time.Second * 20
)

type SimpleConsumer = rmqClient.SimpleConsumer
type MessageView = rmqClient.MessageView
type MessageViewFunc func(*MessageView) bool
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
	Topic             string
	Group             string
	Endpoint          string
	AccessKey         string
	SecretKey         string
	AwaitDuration     time.Duration // maximum waiting time for receive func
	MaxMessageNum     int32         // maximum number of messages received at one time
	InvisibleDuration time.Duration // invisibleDuration should > 20s
}

func NewSimpleConsumer(conf *ConsumerConfig) (Consumer, error) {
	var err error
	consumer := &consumerEntity{ConsumerConfig: conf}
	await := util.If(conf.AwaitDuration > 0, conf.AwaitDuration, DefaultConsumerAwaitDuration)
	consumer.simpleConsumer, err = rmqClient.NewSimpleConsumer(&rmqClient.Config{
		Endpoint:      conf.Endpoint,
		ConsumerGroup: conf.Group,
		Credentials: &credentials.SessionCredentials{
			AccessKey:    conf.AccessKey,
			AccessSecret: conf.SecretKey,
		},
	},
		rmqClient.WithAwaitDuration(await),
		rmqClient.WithSubscriptionExpressions(map[string]*rmqClient.FilterExpression{
			conf.Topic: rmqClient.SUB_ALL,
		}),
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
