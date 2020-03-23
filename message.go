package nkn

import (
	"crypto/rand"
	"errors"

	"github.com/gogo/protobuf/proto"
	"github.com/nknorg/nkn-sdk-go/payloads"
	"golang.org/x/crypto/nacl/box"
)

const (
	nonceSize     = 24
	sharedKeySize = 32
)

// Payload type alias for gomobile compatibility
const (
	BinaryType  = int32(payloads.BINARY)
	TextType    = int32(payloads.TEXT)
	AckType     = int32(payloads.ACK)
	SessionType = int32(payloads.SESSION)
)

type Message struct {
	Src       string
	Data      []byte
	Type      int32
	Encrypted bool
	Pid       []byte
	NoAck     bool
	reply     func(interface{}) error
}

func (msg *Message) Reply(data interface{}) error {
	return msg.reply(data)
}

// ReplyBinary is a wrapper of Reply for gomobile compatibility
func (msg *Message) ReplyBinary(data []byte) error {
	return msg.Reply(data)
}

// ReplyText is a wrapper of Reply for gomobile compatibility
func (msg *Message) ReplyText(data string) error {
	return msg.Reply(data)
}

func encrypt(message []byte, sharedKey *[sharedKeySize]byte) ([]byte, []byte, error) {
	encrypted := make([]byte, len(message)+box.Overhead)
	var nonce [nonceSize]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, nil, err
	}
	box.SealAfterPrecomputation(encrypted[:0], message, &nonce, sharedKey)
	return encrypted, nonce[:], nil
}

func decrypt(message []byte, nonce [nonceSize]byte, sharedKey *[sharedKeySize]byte) ([]byte, error) {
	decrypted := make([]byte, len(message)-box.Overhead)
	_, ok := box.OpenAfterPrecomputation(decrypted[:0], message, &nonce, sharedKey)
	if !ok {
		return nil, errors.New("decrypt message failed")
	}

	return decrypted, nil
}

func newBinaryPayload(data, pid, replyToPid []byte, noAck bool) (*payloads.Payload, error) {
	if len(pid) == 0 && len(replyToPid) == 0 {
		var err error
		pid, err = RandomBytes(MessageIDSize)
		if err != nil {
			return nil, err
		}
	}

	return &payloads.Payload{
		Type:       payloads.BINARY,
		Pid:        pid,
		Data:       data,
		ReplyToPid: replyToPid,
		NoAck:      noAck,
	}, nil
}

func newTextPayload(text string, pid, replyToPid []byte, noAck bool) (*payloads.Payload, error) {
	if len(pid) == 0 && len(replyToPid) == 0 {
		var err error
		pid, err = RandomBytes(MessageIDSize)
		if err != nil {
			return nil, err
		}
	}

	data, err := proto.Marshal(&payloads.TextData{Text: text})
	if err != nil {
		return nil, err
	}

	return &payloads.Payload{
		Type:       payloads.TEXT,
		Pid:        pid,
		Data:       data,
		ReplyToPid: replyToPid,
		NoAck:      noAck,
	}, nil
}

func newAckPayload(replyToPid []byte) (*payloads.Payload, error) {
	return &payloads.Payload{
		Type:       payloads.ACK,
		ReplyToPid: replyToPid,
	}, nil
}

func newMessagePayload(data interface{}, pid []byte, noAck bool) (*payloads.Payload, error) {
	switch v := data.(type) {
	case []byte:
		return newBinaryPayload(v, pid, nil, noAck)
	case string:
		return newTextPayload(v, pid, nil, noAck)
	default:
		return nil, ErrInvalidPayloadType
	}
}

func newReplyPayload(data interface{}, replyToPid []byte) (*payloads.Payload, error) {
	switch v := data.(type) {
	case []byte:
		return newBinaryPayload(v, nil, replyToPid, false)
	case string:
		return newTextPayload(v, nil, replyToPid, false)
	case nil:
		return newAckPayload(replyToPid)
	default:
		return nil, ErrInvalidPayloadType
	}
}
