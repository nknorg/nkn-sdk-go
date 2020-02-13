package nkn

import (
	"crypto/rand"
	"errors"

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
