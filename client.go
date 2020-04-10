package nkn

import (
	"bytes"
	"compress/zlib"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/gorilla/websocket"
	"github.com/nknorg/nkn-sdk-go/payloads"
	"github.com/nknorg/nkn/api/common"
	"github.com/nknorg/nkn/crypto"
	"github.com/nknorg/nkn/crypto/ed25519"
	"github.com/nknorg/nkn/pb"
	"github.com/nknorg/nkn/util/address"
	"github.com/nknorg/nkn/util/config"
	"github.com/patrickmn/go-cache"
	"golang.org/x/crypto/nacl/box"
)

const (
	// MessageIDSize is the default message id size in bytes
	MessageIDSize = 8

	// Max client message size in bytes. NKN node is using 4*1024*1024 as limit
	// (config.MaxClientMessageSize), we give some additional space for
	// serialization overhead.
	maxClientMessageSize = 4000000

	// WebSocket ping interval.
	pingInterval = 8 * time.Second

	// WebSocket ping response timeout. Should be greater than pingInterval.
	pongTimeout = 10 * time.Second

	// Should match the setClient action string on node side.
	setClientAction = "setClient"
)

// Client sends and receives data between any NKN clients regardless their
// network condition without setting up a server or relying on any third party
// services. Data are end to end encrypted by default. Typically you might want
// to use multiclient instead of using client directly.
type Client struct {
	OnConnect *OnConnect // Event emitting channel when client connects to node and becomes ready to send messages. One should only use the first event of the channel.
	OnMessage *OnMessage // Event emitting channel when client receives a message (not including reply or ACK).

	config            *ClientConfig
	account           *Account
	publicKey         []byte
	curveSecretKey    *[sharedKeySize]byte
	address           string
	addressID         []byte
	sigChainBlockHash string
	reconnectChan     chan struct{}
	responseChannels  *cache.Cache

	lock       sync.RWMutex
	closed     bool
	conn       *websocket.Conn
	node       *Node
	urlString  string
	sharedKeys map[string]*[sharedKeySize]byte
}

// clientInterface is the common interface of client and multiclient.
type clientInterface interface {
	getConfig() *ClientConfig
	send(dests []string, payload *payloads.Payload, encrypted bool, maxHoldingSeconds int32) error
}

type setClientResult struct {
	Node              *Node  `json:"node"`
	SigChainBlockHash string `json:"sigChainBlockHash"`
}

// NewClient creates a client with an account, an optional identifier, and a
// optional client config. For any zero value field in config, the default
// client config value will be used. If config is nil, the default client config
// will be used.
func NewClient(account *Account, identifier string, config *ClientConfig) (*Client, error) {
	config, err := MergeClientConfig(config)
	if err != nil {
		return nil, err
	}

	pk := account.PubKey()
	addr := address.MakeAddressString(pk, identifier)

	var sk [ed25519.PrivateKeySize]byte
	copy(sk[:], account.PrivKey())
	curveSecretKey := ed25519.PrivateKeyToCurve25519PrivateKey(&sk)

	c := Client{
		config:           config,
		account:          account,
		publicKey:        pk,
		curveSecretKey:   curveSecretKey,
		address:          addr,
		addressID:        addressToID(addr),
		OnConnect:        NewOnConnect(1, nil),
		OnMessage:        NewOnMessage(int(config.MsgChanLen), nil),
		reconnectChan:    make(chan struct{}, 1),
		responseChannels: cache.New(time.Duration(config.MsgCacheExpiration)*time.Millisecond, time.Duration(config.MsgCacheExpiration)*time.Millisecond),
		sharedKeys:       make(map[string]*[sharedKeySize]byte),
	}

	go c.handleReconnect()

	err = c.connect(int(c.config.ConnectRetries))
	if err != nil {
		return nil, err
	}

	return &c, nil
}

// Seed returns the secret seed of the client. Secret seed can be used to create
// client/wallet with the same key pair and should be kept secret and safe.
func (c *Client) Seed() []byte {
	return c.account.Seed()
}

// PubKey returns the public key of the client.
func (c *Client) PubKey() []byte {
	return c.account.PubKey()
}

// Address returns the NKN client address of the client. Client address is in
// the form of
//   identifier.pubKeyHex
// if identifier is not an empty string, or
//   pubKeyHex
// if identifier is an empty string.
//
// Note that client address is different from wallet address using the same key
// pair (account). Wallet address can be computed from client address, but NOT
// vice versa.
func (c *Client) Address() string {
	return c.address
}

// IsClosed returns whether the client is closed and should not be used anymore.
func (c *Client) IsClosed() bool {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return c.closed
}

// Close closes the client.
func (c *Client) Close() {
	c.lock.Lock()
	defer c.lock.Unlock()
	if !c.closed {
		c.closed = true
		close(c.OnConnect.C)
		close(c.OnMessage.C)
		close(c.reconnectChan)
		c.conn.Close()
	}
}

// GetNode returns the node that client is currently connected to.
func (c *Client) GetNode() *Node {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return c.node
}

// GetConn returns the current websocket connection client is using.
func (c *Client) GetConn() *websocket.Conn {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return c.conn
}

func (c *Client) getOrComputeSharedKey(remotePublicKey []byte) (*[sharedKeySize]byte, error) {
	c.lock.RLock()
	sharedKey, ok := c.sharedKeys[string(remotePublicKey)]
	c.lock.RUnlock()
	if ok && sharedKey != nil {
		return sharedKey, nil
	}

	if len(remotePublicKey) != ed25519.PublicKeySize {
		return nil, ErrInvalidPubkeySize
	}

	var pk [ed25519.PublicKeySize]byte
	copy(pk[:], remotePublicKey)
	curve25519PublicKey, ok := ed25519.PublicKeyToCurve25519PublicKey(&pk)
	if !ok {
		return nil, ErrInvalidPubkey
	}

	sharedKey = new([sharedKeySize]byte)
	box.Precompute(sharedKey, curve25519PublicKey, c.curveSecretKey)

	c.lock.Lock()
	c.sharedKeys[string(remotePublicKey)] = sharedKey
	c.lock.Unlock()

	return sharedKey, nil
}

func (c *Client) encryptPayload(msg *payloads.Payload, dests []string) ([][]byte, error) {
	rawPayload, err := proto.Marshal(msg)
	if err != nil {
		return nil, err
	}

	if len(dests) > 1 {
		var key [sharedKeySize]byte
		if _, err := rand.Read(key[:]); err != nil {
			return nil, err
		}

		encrypted, msgNonce, err := encrypt(rawPayload, &key)
		if err != nil {
			return nil, err
		}

		msgs := make([][]byte, len(dests))

		for i, dest := range dests {
			_, destPubkey, _, err := address.ParseClientAddress(dest)
			if err != nil {
				return nil, err
			}

			sharedKey, err := c.getOrComputeSharedKey(destPubkey)
			if err != nil {
				return nil, err
			}

			encryptedKey, keyNonce, err := encrypt(key[:], sharedKey)
			if err != nil {
				return nil, err
			}

			nonce := append(keyNonce, msgNonce...)

			msgs[i], err = proto.Marshal(&payloads.Message{
				Payload:      encrypted,
				Encrypted:    true,
				Nonce:        nonce,
				EncryptedKey: encryptedKey,
			})
			if err != nil {
				return nil, err
			}
		}

		return msgs, nil
	}

	_, destPubkey, _, err := address.ParseClientAddress(dests[0])
	if err != nil {
		return nil, err
	}

	sharedKey, err := c.getOrComputeSharedKey(destPubkey)
	if err != nil {
		return nil, err
	}

	encrypted, nonce, err := encrypt(rawPayload, sharedKey)
	if err != nil {
		return nil, err
	}

	data, err := proto.Marshal(&payloads.Message{
		Payload:      encrypted,
		Encrypted:    true,
		Nonce:        nonce,
		EncryptedKey: nil,
	})
	if err != nil {
		return nil, err
	}
	return [][]byte{data}, nil
}

func (c *Client) decryptPayload(msg *payloads.Message, srcAddr string) ([]byte, error) {
	rawPayload := msg.Payload
	_, srcPubkey, _, err := address.ParseClientAddress(srcAddr)
	if err != nil {
		return nil, err
	}

	encryptedKey := msg.EncryptedKey
	var decrypted []byte
	if encryptedKey != nil && len(encryptedKey) > 0 {
		var keyNonce, msgNonce [nonceSize]byte
		copy(keyNonce[:], msg.Nonce[:nonceSize])
		copy(msgNonce[:], msg.Nonce[nonceSize:])

		sharedKey, err := c.getOrComputeSharedKey(srcPubkey)
		if err != nil {
			return nil, err
		}

		decryptedKeySlice, err := decrypt(encryptedKey, keyNonce, sharedKey)
		if err != nil {
			return nil, err
		}
		var decryptedKey [sharedKeySize]byte
		copy(decryptedKey[:], decryptedKeySlice)

		decrypted, err = decrypt(rawPayload, msgNonce, &decryptedKey)
		if err != nil {
			return nil, err
		}
	} else {
		var nonce [nonceSize]byte
		copy(nonce[:], msg.Nonce)

		sharedKey, err := c.getOrComputeSharedKey(srcPubkey)
		if err != nil {
			return nil, err
		}

		decrypted, err = decrypt(rawPayload, nonce, sharedKey)
		if err != nil {
			return nil, err
		}
	}

	return decrypted, nil
}

func (c *Client) handleMessage(msgType int, data []byte) error {
	if c.IsClosed() {
		return nil
	}

	switch msgType {
	case websocket.TextMessage:
		msg := make(map[string]*json.RawMessage)
		if err := json.Unmarshal(data, &msg); err != nil {
			return err
		}
		var action string
		if err := json.Unmarshal(*msg["Action"], &action); err != nil {
			return err
		}
		var errCode common.ErrCode
		if err := json.Unmarshal(*msg["Error"], &errCode); err != nil {
			return err
		}
		if errCode != common.SUCCESS {
			if errCode == common.WRONG_NODE {
				var node Node
				if err := json.Unmarshal(*msg["Result"], &node); err != nil {
					return err
				}
				go func() {
					err := c.connectToNode(&node)
					if err != nil {
						c.reconnect()
					}
				}()
			} else if action == setClientAction {
				c.Close()
			}
			return errorWithCode{
				err:  errors.New(common.ErrMessage[errCode]),
				code: int32(errCode),
			}
		}
		switch action {
		case setClientAction:
			var result setClientResult
			if err := json.Unmarshal(*msg["Result"], &result); err != nil {
				return err
			}
			c.sigChainBlockHash = result.SigChainBlockHash

			c.lock.RLock()
			defer c.lock.RUnlock()
			if c.closed {
				return nil
			}

			node := c.GetNode()
			c.OnConnect.receive(node)
		case "updateSigChainBlockHash":
			var sigChainBlockHash string
			if err := json.Unmarshal(*msg["Result"], &sigChainBlockHash); err != nil {
				return err
			}
			c.sigChainBlockHash = sigChainBlockHash
		}
	case websocket.BinaryMessage:
		clientMsg := &pb.ClientMessage{}
		if err := proto.Unmarshal(data, clientMsg); err != nil {
			return err
		}
		switch clientMsg.MessageType {
		case pb.INBOUND_MESSAGE:
			inboundMsg := &pb.InboundMessage{}
			if err := proto.Unmarshal(clientMsg.Message, inboundMsg); err != nil {
				return err
			}

			if len(inboundMsg.PrevSignature) > 0 {
				go func() {
					if err := c.sendReceipt(inboundMsg.PrevSignature); err != nil {
						log.Println(err)
					}
				}()
			}

			payloadMsg := &payloads.Message{}
			if err := proto.Unmarshal(inboundMsg.Payload, payloadMsg); err != nil {
				return err
			}
			var payloadBytes []byte
			if payloadMsg.Encrypted {
				var err error
				payloadBytes, err = c.decryptPayload(payloadMsg, inboundMsg.Src)
				if err != nil {
					return err
				}
			} else {
				payloadBytes = payloadMsg.Payload
			}
			payload := &payloads.Payload{}
			if err := proto.Unmarshal(payloadBytes, payload); err != nil {
				return err
			}
			data := payload.Data
			switch payload.Type {
			case payloads.TEXT:
				textData := &payloads.TextData{}
				if err := proto.Unmarshal(data, textData); err != nil {
					return err
				}
				data = []byte(textData.Text)
			case payloads.ACK:
				data = nil
			}

			var msg *Message
			switch payload.Type {
			case payloads.BINARY, payloads.TEXT, payloads.SESSION:
				msg = &Message{
					Src:       inboundMsg.Src,
					Data:      data,
					Type:      int32(payload.Type),
					Encrypted: payloadMsg.Encrypted,
					MessageID: payload.MessageId,
					NoReply:   payload.NoReply,
				}
			}

			if len(payload.ReplyToId) > 0 {
				msgIDString := string(payload.ReplyToId)
				onReply, ok := c.responseChannels.Get(msgIDString)
				if ok {
					c.responseChannels.Delete(msgIDString)
					onReply.(*OnMessage).receive(msg, false)
				}
				return nil
			}

			if msg == nil {
				return nil
			}

			if payload.NoReply {
				msg.reply = func(data interface{}) error {
					return nil
				}
			} else {
				msg.reply = func(data interface{}) error {
					payload, err := newReplyPayload(data, payload.MessageId)
					if err != nil {
						return err
					}
					if err := c.send([]string{inboundMsg.Src}, payload, payloadMsg.Encrypted, 0); err != nil {
						return err
					}
					return nil
				}
			}

			c.lock.RLock()
			defer c.lock.RUnlock()
			if c.closed {
				return nil
			}

			c.OnMessage.receive(msg, true)
		}
	}

	return nil
}

func (c *Client) connectToNode(node *Node) error {
	urlString := (&url.URL{Scheme: "ws", Host: node.Address}).String()
	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = time.Duration(c.config.WsHandshakeTimeout) * time.Millisecond

	conn, _, err := dialer.Dial(urlString, nil)
	if err != nil {
		return err
	}

	c.lock.Lock()
	prevConn := c.conn
	c.conn = conn
	c.node = node
	c.urlString = urlString
	c.lock.Unlock()

	if prevConn != nil {
		prevConn.Close()
	}

	conn.SetReadLimit(config.MaxClientMessageSize)
	conn.SetReadDeadline(time.Now().Add(pongTimeout))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongTimeout))
		return nil
	})

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()
		var err error
		for {
			select {
			case <-ticker.C:
				c.lock.Lock()
				conn.SetWriteDeadline(time.Now().Add(pingInterval))
				err = conn.WriteMessage(websocket.PingMessage, nil)
				c.lock.Unlock()
				if err != nil {
					return
				}
			case <-done:
				return
			}
		}
	}()

	go func() {
		req := make(map[string]interface{})
		req["Action"] = "setClient"
		req["Addr"] = c.Address()

		c.lock.Lock()
		err := conn.WriteJSON(req)
		c.lock.Unlock()
		if err != nil {
			log.Println(err)
			c.reconnect()
			return
		}
	}()

	go func() {
		defer close(done)
		for {
			if c.IsClosed() {
				return
			}

			msgType, data, err := conn.ReadMessage()
			if err != nil && !c.IsClosed() {
				log.Println(err)
				c.reconnect()
				return
			}

			conn.SetReadDeadline(time.Now().Add(pongTimeout))

			err = c.handleMessage(msgType, data)
			if err != nil {
				log.Println(err)
				continue
			}
		}
	}()

	return nil
}

func (c *Client) connect(maxRetries int) error {
	retryInterval := c.config.MinReconnectInterval
	for retry := 1; maxRetries == 0 || retry <= maxRetries; retry++ {
		if retry > 1 {
			log.Printf("Retry in %v...\n", retryInterval)
			time.Sleep(time.Duration(retryInterval) * time.Millisecond)
			retryInterval *= 2
			if retryInterval > c.config.MaxReconnectInterval {
				retryInterval = c.config.MaxReconnectInterval
			}
		}

		node, err := GetWsAddr(c.Address(), c.config)
		if err != nil {
			log.Println(err)
			continue
		}

		err = c.connectToNode(node)
		if err != nil {
			log.Println(err)
			continue
		}

		return nil
	}

	return ErrConnectFailed
}

func (c *Client) reconnect() {
	if c.IsClosed() {
		return
	}
	select {
	case c.reconnectChan <- struct{}{}:
	default:
	}
}

func (c *Client) handleReconnect() {
	for range c.reconnectChan {
		if c.IsClosed() {
			return
		}

		log.Printf("Reconnect in %v...\n", c.config.MinReconnectInterval)
		time.Sleep(time.Duration(c.config.MinReconnectInterval) * time.Millisecond)

		err := c.connect(0)
		if err != nil {
			log.Println(err)
			c.Close()
			return
		}
	}
}

func (c *Client) writeMessage(buf []byte) error {
	c.lock.Lock()
	c.conn.SetWriteDeadline(time.Now().Add(time.Duration(c.config.WsWriteTimeout) * time.Millisecond))
	err := c.conn.WriteMessage(websocket.BinaryMessage, buf)
	c.lock.Unlock()
	if err != nil {
		c.reconnect()
	}
	return err
}

func (c *Client) sendReceipt(prevSignature []byte) error {
	sigChainElem := &pb.SigChainElem{}
	buff := bytes.NewBuffer(nil)
	if err := sigChainElem.SerializationUnsigned(buff); err != nil {
		return err
	}
	sigChainElemSerialized := buff.Bytes()

	digest := sha256.Sum256(prevSignature)
	digest = sha256.Sum256(append(digest[:], sigChainElemSerialized...))
	signature, err := crypto.Sign(c.account.PrivateKey, digest[:])
	if err != nil {
		return err
	}

	receipt := &pb.Receipt{
		PrevSignature: prevSignature,
		Signature:     signature,
	}
	receiptData, err := proto.Marshal(receipt)
	if err != nil {
		return err
	}

	clientMsg := &pb.ClientMessage{
		MessageType:     pb.RECEIPT,
		Message:         receiptData,
		CompressionType: pb.COMPRESSION_NONE,
	}
	buf, err := proto.Marshal(clientMsg)
	if err != nil {
		return err
	}

	return c.writeMessage(buf)
}

// Send sends bytes or string data to one or multiple destinations with an
// optional config. Returned OnMessage channel will emit if a reply or ACK for
// this message is received.
func (c *Client) Send(dests *StringArray, data interface{}, config *MessageConfig) (*OnMessage, error) {
	config, err := MergeMessageConfig(c.config.MessageConfig, config)
	if err != nil {
		return nil, err
	}

	payload, err := newMessagePayload(data, config.MessageID, config.NoReply)
	if err != nil {
		return nil, err
	}

	if err := c.send(dests.Elems, payload, !config.Unencrypted, config.MaxHoldingSeconds); err != nil {
		return nil, err
	}

	onReply := NewOnMessage(1, nil)
	if !config.NoReply {
		c.responseChannels.Add(string(payload.MessageId), onReply, cache.DefaultExpiration)
	}

	return onReply, nil
}

// SendBinary is a wrapper of Send without interface type for gomobile
// compatibility.
func (c *Client) SendBinary(dests *StringArray, data []byte, config *MessageConfig) (*OnMessage, error) {
	return c.Send(dests, data, config)
}

// SendText is a wrapper of Send without interface type for gomobile
// compatibility.
func (c *Client) SendText(dests *StringArray, data string, config *MessageConfig) (*OnMessage, error) {
	return c.Send(dests, data, config)
}

func (c *Client) processDest(dest string) (string, error) {
	if len(dest) == 0 {
		return "", ErrNoDestination
	}
	addr := strings.Split(dest, ".")
	if len(addr[len(addr)-1]) < 2*ed25519.PublicKeySize {
		reg, err := GetRegistrant(addr[len(addr)-1], c.config)
		if err != nil {
			return "", err
		}
		if len(reg.Registrant) == 0 {
			return "", ErrInvalidPubkeyOrName
		}
		addr[len(addr)-1] = reg.Registrant
	}
	return strings.Join(addr, "."), nil
}

func (c *Client) processDests(dests []string) ([]string, error) {
	processedDests := make([]string, 0, len(dests))
	for _, dest := range dests {
		processedDest, err := c.processDest(dest)
		if err != nil {
			log.Println(err)
			continue
		}
		processedDests = append(processedDests, processedDest)
	}
	if len(processedDests) == 0 {
		return nil, ErrInvalidDestination
	}
	return processedDests, nil
}

func (c *Client) newPayloads(dests []string, payload *payloads.Payload, encrypted bool) ([][]byte, error) {
	if encrypted {
		return c.encryptPayload(payload, dests)
	}

	payloadData, err := proto.Marshal(payload)
	if err != nil {
		return nil, err
	}

	data, err := proto.Marshal(&payloads.Message{
		Payload:      payloadData,
		Encrypted:    false,
		Nonce:        nil,
		EncryptedKey: nil,
	})
	if err != nil {
		return nil, err
	}

	return [][]byte{data}, nil
}

func (c *Client) newOutboundMessage(dests []string, plds [][]byte, encrypted bool, maxHoldingSeconds int32) (*pb.OutboundMessage, error) {
	outboundMsg := &pb.OutboundMessage{
		Dests:             dests,
		Payloads:          plds,
		MaxHoldingSeconds: uint32(maxHoldingSeconds),
	}

	nodePk, err := hex.DecodeString(c.node.PublicKey)
	if err != nil {
		return nil, err
	}
	sigChainElem := &pb.SigChainElem{
		NextPubkey: nodePk,
	}
	buff := bytes.NewBuffer(nil)
	if err := sigChainElem.SerializationUnsigned(buff); err != nil {
		return nil, err
	}
	sigChainElemSerialized := buff.Bytes()

	nonce := randUint32()

	sigChain := pb.SigChain{
		Nonce:     nonce,
		SrcId:     c.addressID,
		SrcPubkey: c.publicKey,
		Elems:     []*pb.SigChainElem{sigChainElem},
	}

	if c.sigChainBlockHash != "" {
		sigChainBlockHash, err := hex.DecodeString(c.sigChainBlockHash)
		if err != nil {
			return nil, err
		}
		sigChain.BlockHash = sigChainBlockHash
		outboundMsg.BlockHash = sigChainBlockHash
	}

	var signatures [][]byte

	for i, dest := range dests {
		destID, destPubKey, _, err := address.ParseClientAddress(dest)
		if err != nil {
			return nil, err
		}
		sigChain.DestId = destID
		sigChain.DestPubkey = destPubKey
		if len(plds) > 1 {
			sigChain.DataSize = uint32(len(plds[i]))
		} else {
			sigChain.DataSize = uint32(len(plds[0]))
		}
		buff := bytes.NewBuffer(nil)
		if err := sigChain.SerializationMetadata(buff); err != nil {
			return nil, err
		}
		digest := sha256.Sum256(buff.Bytes())
		digest = sha256.Sum256(append(digest[:], sigChainElemSerialized...))
		signature, err := crypto.Sign(c.account.PrivateKey, digest[:])
		if err != nil {
			return nil, err
		}
		signatures = append(signatures, signature)
	}

	outboundMsg.Signatures = signatures
	outboundMsg.Nonce = nonce
	return outboundMsg, nil
}

func (c *Client) newClientMessage(outboundMsg *pb.OutboundMessage) (*pb.ClientMessage, error) {
	clientMsg := &pb.ClientMessage{
		MessageType: pb.OUTBOUND_MESSAGE,
	}

	outboundMsgData, err := proto.Marshal(outboundMsg)
	if err != nil {
		return nil, err
	}

	if len(outboundMsg.Payloads) > 1 {
		clientMsg.CompressionType = pb.COMPRESSION_ZLIB
		var b bytes.Buffer
		w := zlib.NewWriter(&b)
		_, err = w.Write(outboundMsgData)
		if err != nil {
			return nil, err
		}
		err = w.Close()
		if err != nil {
			return nil, err
		}
		clientMsg.Message = b.Bytes()
	} else {
		clientMsg.CompressionType = pb.COMPRESSION_NONE
		clientMsg.Message = outboundMsgData
	}

	return clientMsg, nil
}

func (c *Client) sendTimeout(dests []string, payload *payloads.Payload, encrypted bool, maxHoldingSeconds int32, writeTimeout time.Duration) error {
	if maxHoldingSeconds < 0 {
		maxHoldingSeconds = 0
	}

	dests, err := c.processDests(dests)
	if err != nil {
		return err
	}

	plds, err := c.newPayloads(dests, payload, encrypted)
	if err != nil {
		return err
	}

	outboundMsgs := make([]*pb.OutboundMessage, 0, 1)
	destList := make([]string, 0, len(dests))
	pldList := make([][]byte, 0, len(plds))
	if len(plds) > 1 {
		var totalSize, size int
		for i := range plds {
			size = len(plds[i]) + len(dests[i]) + ed25519.SignatureSize
			if size > maxClientMessageSize {
				return ErrMessageOversize
			}
			if totalSize+size > maxClientMessageSize {
				outboundMsg, err := c.newOutboundMessage(destList, pldList, encrypted, maxHoldingSeconds)
				if err != nil {
					return err
				}
				outboundMsgs = append(outboundMsgs, outboundMsg)
				destList = make([]string, 0, len(destList))
				pldList = make([][]byte, 0, len(pldList))
				totalSize = 0
			}
			destList = append(destList, dests[i])
			pldList = append(pldList, plds[i])
			totalSize += size
		}
	} else {
		size := len(plds[0])
		for i := range dests {
			size += len(dests[i]) + ed25519.SignatureSize
		}
		if size > maxClientMessageSize {
			return ErrMessageOversize
		}
		destList = dests
		pldList = plds
	}

	outboundMsg, err := c.newOutboundMessage(destList, pldList, encrypted, maxHoldingSeconds)
	if err != nil {
		return err
	}
	outboundMsgs = append(outboundMsgs, outboundMsg)

	if len(outboundMsgs) > 1 {
		log.Printf("Client message size is greater than %d bytes, split into %d batches.", maxClientMessageSize, len(outboundMsgs))
	}

	for _, outboundMsg := range outboundMsgs {
		clientMsg, err := c.newClientMessage(outboundMsg)
		if err != nil {
			return err
		}

		buf, err := proto.Marshal(clientMsg)
		if err != nil {
			return err
		}

		err = c.writeMessage(buf)
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *Client) send(dests []string, payload *payloads.Payload, encrypted bool, maxHoldingSeconds int32) error {
	return c.sendTimeout(dests, payload, encrypted, maxHoldingSeconds, time.Duration(c.config.WsWriteTimeout)*time.Millisecond)
}

func publish(c clientInterface, topic string, data interface{}, config *MessageConfig) error {
	config, err := MergeMessageConfig(c.getConfig().MessageConfig, config)
	if err != nil {
		return err
	}

	payload, err := newMessagePayload(data, config.MessageID, true)
	if err != nil {
		return err
	}

	offset := int(config.Offset)
	limit := int(config.Limit)
	res, err := GetSubscribers(topic, offset, limit, false, config.TxPool, c.getConfig())
	if err != nil {
		return err
	}

	subscribers := res.Subscribers.Map
	subscribersInTxPool := res.SubscribersInTxPool.Map

	dests := make([]string, 0, len(subscribers)+len(subscribersInTxPool))
	for subscriber := range subscribers {
		dests = append(dests, subscriber)
	}

	for len(subscribers) >= limit {
		offset += limit
		res, err = GetSubscribers(topic, offset, limit, false, false, c.getConfig())
		if err != nil {
			return err
		}
		for subscriber := range res.Subscribers.Map {
			dests = append(dests, subscriber)
		}
	}

	if config.TxPool {
		for subscriber := range subscribersInTxPool {
			dests = append(dests, subscriber)
		}
	}

	return c.send(dests, payload, !config.Unencrypted, config.MaxHoldingSeconds)
}

// Publish sends bytes or string data to all subscribers of a topic with an
// optional config.
func (c *Client) Publish(topic string, data interface{}, config *MessageConfig) error {
	return publish(c, topic, data, config)
}

// PublishBinary is a wrapper of Publish without interface type for gomobile
// compatibility.
func (c *Client) PublishBinary(topic string, data []byte, config *MessageConfig) error {
	return c.Publish(topic, data, config)
}

// PublishText is a wrapper of Publish without interface type for gomobile
// compatibility.
func (c *Client) PublishText(topic string, data string, config *MessageConfig) error {
	return c.Publish(topic, data, config)
}

// SetWriteDeadline sets the write deadline of the websocket connection.
func (c *Client) SetWriteDeadline(deadline time.Time) error {
	c.lock.Lock()
	defer c.lock.Unlock()
	if c.conn == nil {
		return ErrNilWebsocketConn
	}
	return c.conn.SetWriteDeadline(deadline)
}

func (c *Client) getConfig() *ClientConfig {
	return c.config
}

// GetSubscribers gets the subscribers of a topic with a offset and max number
// of results (limit). If meta is true, results contain each subscriber's
// metadata. If txPool is true, results contain subscribers in txPool. Enabling
// this will get subscribers sooner after they send subscribe transactions, but
// might affect the correctness of subscribers because transactions in txpool is
// not guaranteed to be packed into a block.
//
// Offset and limit are changed to signed int for gomobile compatibility
func (c *Client) GetSubscribers(topic string, offset, limit int, meta, txPool bool) (*Subscribers, error) {
	return GetSubscribers(topic, offset, limit, meta, txPool, c.config)
}

// GetSubscription gets the subscription details of a subscriber in a topic.
func (c *Client) GetSubscription(topic string, subscriber string) (*Subscription, error) {
	return GetSubscription(topic, subscriber, c.config)
}

// GetSubscribersCount returns the number of subscribers of a topic (not
// including txPool).
//
// Count is changed to signed int for gomobile compatibility
func (c *Client) GetSubscribersCount(topic string) (int, error) {
	return GetSubscribersCount(topic, c.config)
}
