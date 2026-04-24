package kafka

import "errors"

var (
	ErrNilConsumerConfig  = errors.New("kafka: consumer config is required")
	ErrContextRequired    = errors.New("kafka: context is required")
	ErrBrokersRequired    = errors.New("kafka: brokers are required")
	ErrTopicRequired      = errors.New("kafka: topic is required")
	ErrConsumerGroupEmpty = errors.New("kafka: consumer group is required")
	ErrSASLConfigInvalid  = errors.New("kafka: username and password must be configured together")
	ErrMessageViewNil     = errors.New("kafka: message view is required")
)
