package wsx

import "errors"

var (
	ErrContextRequired           = errors.New("wsx: context is required")
	ErrConnectionClosed          = errors.New("wsx: connection closed")
	ErrEndpointAlreadyStarted    = errors.New("wsx: endpoint already started")
	ErrEndpointNotStarted        = errors.New("wsx: endpoint not started")
	ErrEndpointClosed            = errors.New("wsx: endpoint closed")
	ErrEndpointLoggerRequired    = errors.New("wsx: endpoint logger is required")
	ErrPrepareRequired           = errors.New("wsx: prepare function is required")
	ErrHubRequired               = errors.New("wsx: hub is required")
	ErrHubNotStarted             = errors.New("wsx: hub not started")
	ErrHubClosed                 = errors.New("wsx: hub closed")
	ErrConnectionRequired        = errors.New("wsx: connection is required")
	ErrRoomMembershipUnsupported = errors.New("wsx: connection does not support room membership")
	ErrRoomRequired              = errors.New("wsx: room is required")
	ErrSessionDuplicate          = errors.New("wsx: session already registered")
	ErrSessionIDRequired         = errors.New("wsx: session id is required")
	ErrSessionNotFound           = errors.New("wsx: session not found")
	ErrUserIDRequired            = errors.New("wsx: user id is required")
	ErrRoomLimitExceeded         = errors.New("wsx: room limit exceeded")
	ErrBrokerClosed              = errors.New("wsx: broker closed")
	ErrBrokerClientRequired      = errors.New("wsx: broker redis client is required")
	ErrBrokerHandlerRequired     = errors.New("wsx: subscribe handler is required")
)
