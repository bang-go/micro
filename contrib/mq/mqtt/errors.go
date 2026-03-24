package mqtt

import "errors"

var (
	ErrNilConfig              = errors.New("mqtt: config is required")
	ErrContextRequired        = errors.New("mqtt: context is required")
	ErrBrokerRequired         = errors.New("mqtt: at least one broker is required")
	ErrClientIDRequired       = errors.New("mqtt: client id is required")
	ErrUsernameRequired       = errors.New("mqtt: username is required")
	ErrPasswordRequired       = errors.New("mqtt: password is required")
	ErrTopicRequired          = errors.New("mqtt: topic is required")
	ErrFiltersRequired        = errors.New("mqtt: filters are required")
	ErrNoTopics               = errors.New("mqtt: at least one topic is required")
	ErrInvalidAliyunAuthMode  = errors.New("mqtt: invalid aliyun auth mode")
	ErrDuplicateFilterTopic   = errors.New("mqtt: duplicate filter topic after normalization")
	ErrOperationTokenRequired = errors.New("mqtt: operation token is required")
)
