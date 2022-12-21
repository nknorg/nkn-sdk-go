package nkn

import (
	"crypto/rand"

	"github.com/golang/protobuf/proto"
	"github.com/nknorg/nkn-sdk-go/payloads"
	"golang.org/x/crypto/nacl/box"
)

const (
	nonceSize     = 24
	sharedKeySize = 32
)

// Payload type alias for gomobile compatibility.
const (
	BinaryType  = int32(payloads.PayloadType_BINARY)
	TextType    = int32(payloads.PayloadType_TEXT)
	AckType     = int32(payloads.PayloadType_ACK)
	SessionType = int32(payloads.PayloadType_SESSION)
)

// Message contains the info of received message.
type Message struct {
	Src       string // Sender's NKN client address.
	Data      []byte // Message data. If data type is string, one can call string() to convert it to original string data.
	Type      int32  // Message data type.
	Encrypted bool   // Whether message is encrypted.
	MessageID []byte // Message ID.
	NoReply   bool   // Indicating no reply or ACK should be sent.
	reply     func(interface{}) error
}

// Reply sends bytes or string data as reply to message sender.
func (msg *Message) Reply(data interface{}) error {
	return msg.reply(data)
}

// ReplyBinary is a wrapper of Reply without interface type for gomobile
// compatibility.
func (msg *Message) ReplyBinary(data []byte) error {
	return msg.Reply(data)
}

// ReplyText is a wrapper of Reply without interface type for gomobile
// compatibility.
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
		return nil, ErrDecryptFailed
	}

	return decrypted, nil
}

func newBinaryPayload(data, messageID, replyToID []byte, noReply bool) (*payloads.Payload, error) {
	if len(messageID) == 0 && len(replyToID) == 0 {
		var err error
		messageID, err = RandomBytes(MessageIDSize)
		if err != nil {
			return nil, err
		}
	}

	return &payloads.Payload{
		Type:      payloads.PayloadType_BINARY,
		MessageId: messageID,
		Data:      data,
		ReplyToId: replyToID,
		NoReply:   noReply,
	}, nil
}

func newTextPayload(text string, messageID, replyToID []byte, noReply bool) (*payloads.Payload, error) {
	if len(messageID) == 0 && len(replyToID) == 0 {
		var err error
		messageID, err = RandomBytes(MessageIDSize)
		if err != nil {
			return nil, err
		}
	}

	data, err := proto.Marshal(&payloads.TextData{Text: text})
	if err != nil {
		return nil, err
	}

	return &payloads.Payload{
		Type:      payloads.PayloadType_TEXT,
		MessageId: messageID,
		Data:      data,
		ReplyToId: replyToID,
		NoReply:   noReply,
	}, nil
}

func newAckPayload(replyToID []byte) (*payloads.Payload, error) {
	return &payloads.Payload{
		Type:      payloads.PayloadType_ACK,
		ReplyToId: replyToID,
	}, nil
}

func newMessagePayload(data interface{}, messageID []byte, noReply bool) (*payloads.Payload, error) {
	switch v := data.(type) {
	case []byte:
		return newBinaryPayload(v, messageID, nil, noReply)
	case string:
		return newTextPayload(v, messageID, nil, noReply)
	default:
		return nil, ErrInvalidPayloadType
	}
}

func NewReplyPayload(data interface{}, replyToID []byte) (*payloads.Payload, error) {
	switch v := data.(type) {
	case []byte:
		return newBinaryPayload(v, nil, replyToID, false)
	case string:
		return newTextPayload(v, nil, replyToID, false)
	case nil:
		return newAckPayload(replyToID)
	default:
		return nil, ErrInvalidPayloadType
	}
}
