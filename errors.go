package nkn_sdk_go

var (
	Closed                = GenericError{err: "closed", timeout: false, temporary: false}
	DialTimeout           = GenericError{err: "dial timeout", timeout: true, temporary: true}
	SessionEstablished    = GenericError{err: "session is already established", timeout: false, temporary: false}
	SessionNotEstablished = GenericError{err: "session not established yet", timeout: false, temporary: true}
	SessionClosed         = GenericError{err: "session closed", timeout: false, temporary: false}
	ReadDeadlineExceeded  = GenericError{err: "read deadline exceeded", timeout: true, temporary: true}
	WriteDeadlineExceeded = GenericError{err: "write deadline exceeded", timeout: true, temporary: true}
	BufferSizeTooSmall    = GenericError{err: "read buffer size is less than data length in non-session mode", timeout: false, temporary: true}
	DataSizeTooLarge      = GenericError{err: "data size is greater than session mtu in non-session mode", timeout: false, temporary: true}
)

type GenericError struct {
	err       string
	timeout   bool
	temporary bool
}

func (e GenericError) Error() string   { return e.err }
func (e GenericError) Timeout() bool   { return e.timeout }
func (e GenericError) Temporary() bool { return e.temporary }
