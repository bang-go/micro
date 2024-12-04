package rmq

import (
	"context"
	rmqClient "github.com/apache/rocketmq-clients/golang/v5"
	"github.com/apache/rocketmq-clients/golang/v5/credentials"
	"time"
)

type RProducer = rmqClient.Producer
type Producer interface {
	Start() error
	Close() error
	GetProducer() RProducer
	SendNormalMessage(context.Context, *Message) ([]*SendReceipt, error)
	AsyncSendNormalMessage(context.Context, *Message, AsyncSendHandler)
	SendFifoMessage(context.Context, *Message) ([]*SendReceipt, error)
	SendDelayMessage(context.Context, *Message, time.Time) ([]*SendReceipt, error)
}
type producerEntity struct {
	*ProducerConfig
	producer RProducer
}
type Message = rmqClient.Message
type SendReceipt = rmqClient.SendReceipt
type AsyncSendHandler = func(context.Context, []*SendReceipt, error)
type ProducerConfig struct {
	//Topic     string
	Endpoint  string
	AccessKey string
	SecretKey string
}

func NewProducer(conf *ProducerConfig) (Producer, error) {
	var err error
	producer := &producerEntity{ProducerConfig: conf}
	producer.producer, err = rmqClient.NewProducer(&rmqClient.Config{
		Endpoint: conf.Endpoint,
		Credentials: &credentials.SessionCredentials{
			AccessKey:    conf.AccessKey,
			AccessSecret: conf.SecretKey,
		},
	},
	//rmqClient.WithTopics(conf.Topic),
	)
	return producer, err
}
func (p *producerEntity) Start() error {
	return p.GetProducer().Start()
}

func (p *producerEntity) Close() error {
	return p.GetProducer().GracefulStop()
}

func (p *producerEntity) GetProducer() RProducer {
	return p.producer
}

func (p *producerEntity) SendNormalMessage(ctx context.Context, msg *Message) ([]*SendReceipt, error) {
	return p.GetProducer().Send(ctx, msg)
}
func (p *producerEntity) AsyncSendNormalMessage(ctx context.Context, msg *Message, handler AsyncSendHandler) {
	p.GetProducer().SendAsync(ctx, msg, handler)
	return
}

// SendFifoMessage 发送顺序消息
func (p *producerEntity) SendFifoMessage(ctx context.Context, msg *Message) ([]*SendReceipt, error) {
	msg.SetMessageGroup("fifo")
	return p.GetProducer().Send(ctx, msg)
}

// SendDelayMessage 发送延时消息
func (p *producerEntity) SendDelayMessage(ctx context.Context, msg *Message, delayTimestamp time.Time) ([]*SendReceipt, error) {
	msg.SetDelayTimestamp(delayTimestamp)
	return p.GetProducer().Send(ctx, msg)
}
