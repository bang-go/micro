package rmq

import "errors"

var (
	ErrNilProducerConfig        = errors.New("rmq: producer config is required")
	ErrNilConsumerConfig        = errors.New("rmq: consumer config is required")
	ErrContextRequired          = errors.New("rmq: context is required")
	ErrEndpointRequired         = errors.New("rmq: endpoint is required")
	ErrConsumerGroupEmpty       = errors.New("rmq: consumer group is required")
	ErrSubscriptionMiss         = errors.New("rmq: topic or subscription expressions are required")
	ErrDuplicateSubscriptionKey = errors.New("rmq: duplicate subscription topic after normalization")
	ErrMessageRequired          = errors.New("rmq: message is required")
	ErrTopicRequired            = errors.New("rmq: topic is required")
	ErrMessageGroupEmpty        = errors.New("rmq: message group is required")
	ErrMessageViewNil           = errors.New("rmq: message view is required")
)
