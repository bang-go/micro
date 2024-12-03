package rmq_test

import (
	"context"
	"github.com/bang-go/micro/mq/rmq"
	"log"
	"testing"
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
	defer p.Close()
	msg := &rmq.Message{Body: []byte(""), Topic: ""}
	sendReceipt, err := p.SendNormalMessage(context.TODO(), msg)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(sendReceipt[0].MessageID)
}
