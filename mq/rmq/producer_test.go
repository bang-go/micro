package rmq_test

import (
	"context"
	"log"
	"testing"

	"github.com/bang-go/micro/mq/rmq"
)

func TestProducer(t *testing.T) {
	p, err := rmq.NewProducer(&rmq.ProducerConfig{
		Endpoint:  "",
		AccessKey: "",
		SecretKey: ""})
	if err != nil {
		log.Fatal(err)
	}
	err = p.Start()
	if err != nil {
		log.Fatal(err)
	}
	defer func(p rmq.Producer) {
		_ = p.Close()
	}(p)
	msg := &rmq.Message{Body: []byte(""), Topic: ""}
	sendReceipt, err := p.SendNormalMessage(context.Background(), msg)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(sendReceipt[0].MessageID)
}
