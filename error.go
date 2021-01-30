package nkn

import (
	"errors"
	"fmt"

	ncp "github.com/nknorg/ncp-go"
	"github.com/nknorg/nkn/v2/vault"
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

const (
	errCodeNetworkError int32 = -50001
	errCodeDecodeError  int32 = -50002
	errCodeEncodeError  int32 = -50003
	errCodeCanceled     int32 = -50003
)

// Error definitions.
var (
	ErrClosed               = ncp.NewGenericError("use of closed network connection", true, true) // The error message is meant to be identical to error returned by net package.
	ErrKeyNotInMap          = errors.New("key not in map")                                        // for gomobile
	ErrInvalidPayloadType   = errors.New("invalid payload type")
	ErrConnectFailed        = errors.New("connect failed")
	ErrNoDestination        = errors.New("no destination")
	ErrInvalidDestination   = errors.New("invalid destination")
	ErrInvalidPubkeyOrName  = errors.New("invalid public key or name")
	ErrInvalidPubkeySize    = errors.New("invalid public key size")
	ErrInvalidPubkey        = errors.New("invalid public key")
	ErrMessageOversize      = fmt.Errorf("encoded message is greater than %v bytes", maxClientMessageSize)
	ErrNilWebsocketConn     = errors.New("nil websocket connection")
	ErrDecryptFailed        = errors.New("decrypt message failed")
	ErrAddrNotAllowed       = errors.New("address not allowed")
	ErrCreateClientFailed   = errors.New("failed to create client")
	ErrNilClient            = errors.New("client is nil")
	ErrNotNanoPay           = errors.New("not nano pay transaction")
	ErrWrongRecipient       = errors.New("wrong nano pay recipient")
	ErrNanoPayClosed        = errors.New("use of closed nano pay claimer")
	ErrInsufficientBalance  = errors.New("insufficient balance")
	ErrInvalidAmount        = errors.New("invalid amount")
	ErrExpiredNanoPay       = errors.New("nanopay expired")
	ErrExpiredNanoPayTxn    = errors.New("nanopay transaction expired")
	ErrWrongPassword        = errors.New("wrong password")
	ErrInvalidWalletVersion = fmt.Errorf("invalid wallet version, should be between %v and %v", vault.MinCompatibleWalletVersion, vault.MaxCompatibleWalletVersion)
)
