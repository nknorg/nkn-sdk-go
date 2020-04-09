package nkn

import (
	ncp "github.com/nknorg/ncp-go"
)

// ErrorWithCode is an error interface that implements error and Code()
type ErrorWithCode interface {
	error
	Code() int32
}

type errorWithCode struct {
	err  error
	code int32
}

func (e errorWithCode) Error() string {
	return e.err.Error()
}

func (e errorWithCode) Code() int32 {
	return e.code
}

var (
	ErrClosed             = ncp.NewGenericError("use of closed network connection", true, true) // The error message is meant to be identical to error returned by net package.
	ErrKeyNotInMap        = ncp.NewGenericError("key not in map", false, false)                 // for gomobile
	ErrInvalidPayloadType = ncp.NewGenericError("invalid payload type", false, false)
)
