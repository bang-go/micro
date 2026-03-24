package wsx

import "errors"

var (
	errConnectionClosed             = errors.New("wsx: connection closed")
	errClientAlreadyStarted         = errors.New("wsx: client already started")
	errClientClosed                 = errors.New("wsx: client closed")
	errHubClosed                    = errors.New("wsx: hub closed")
	errHubRoomMembershipUnsupported = errors.New("wsx: connection does not support room membership")
	errHubRoomMissing               = errors.New("wsx: room is required")
	errHubSessionIDMissing          = errors.New("wsx: session id is required")
	errHubSessionNotFound           = errors.New("wsx: session not found")
	errHubUserIDMissing             = errors.New("wsx: user id is required")
	errServerAlreadyRunning         = errors.New("wsx: server already running")
	errServerHandlerMissing         = errors.New("wsx: handler is required")
	errServerAddrMissing            = errors.New("wsx: server addr or listener is required")
	errBrokerClosed                 = errors.New("wsx: broker closed")
	errBrokerHandlerMissing         = errors.New("wsx: subscribe handler is required")
)
