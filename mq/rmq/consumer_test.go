package rmq_test

import (
	"context"
	"log"
	"testing"
	"time"

	"github.com/bang-go/micro/mq/rmq"
)

func TestConsumer(t *testing.T) {
	c, err := rmq.NewSimpleConsumer(&rmq.ConsumerConfig{
		AwaitDuration: 10 * time.Second,
		SubscriptionExpressions: map[string]*rmq.FilterExpression{
			"topic1": rmq.SubAll,
			"topic2": rmq.NewFilterExpression("tag1"),
		},
		Group:     "",
		Endpoint:  "",
		AccessKey: "",
		SecretKey: ""})
	if err != nil {
		log.Fatal(err)
	}
	err = c.Start()
	if err != nil {
		log.Fatal(err)
	}
	defer func(c rmq.Consumer) {
		_ = c.Close()
	}(c)
	for {
		mvs, err := c.Receive()
		if errRpcStatus, ok := rmq.AsErrRpcStatus(err); ok {
			log.Println(errRpcStatus.GetCode(), errRpcStatus.GetMessage())
			if errRpcStatus.GetCode() == int32(rmq.CodeMessageNotFound) {
				log.Println("no message")
			}
		}
		if err != nil {
			log.Println(err)
			continue
		}
		for _, mv := range mvs {
			log.Println(mv)
			err = c.Ack(context.TODO(), mv)
			if err != nil {
				log.Fatal(err)
			}
			log.Println(string(mv.GetBody()))
		}
	}

}
