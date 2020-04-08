package nkn

import (
	ncp "github.com/nknorg/ncp-go"
)

var (
	ErrClosed             = ncp.NewGenericError("use of closed network connection", true, true) // The error message is meant to be identical to error returned by net package.
	ErrKeyNotInMap        = ncp.NewGenericError("key not in map", false, false)                 // for gomobile
	ErrInvalidPayloadType = ncp.NewGenericError("invalid payload type", false, false)
)
