package nkn_sdk_go

import (
	"bytes"
	"compress/zlib"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"

	"github.com/nknorg/nkn/crypto/ed25519"
	"golang.org/x/crypto/nacl/box"

	"github.com/nknorg/nkn-sdk-go/payloads"

	"github.com/gogo/protobuf/proto"
	"github.com/gorilla/websocket"
	"github.com/nknorg/nkn/api/common"
	"github.com/nknorg/nkn/crypto"
	"github.com/nknorg/nkn/pb"
	"github.com/nknorg/nkn/util/address"
	"github.com/nknorg/nkn/vault"
)

const (
	defaultReconnectInterval = time.Second
	defaultMsgChanLen        = 1024
	defaultBlockChanLen      = 1
	defaultConnectRetries    = 3
	handshakeTimeout         = 5 * time.Second
	maxRetryInterval         = time.Minute

	nonceSize     = 24
	sharedKeySize = 32
)

type ClientConfig struct {
	SeedRPCServerAddr string
	ReconnectInterval time.Duration
	MaxHoldingSeconds uint32
	MsgChanLen        uint32
	BlockChanLen      uint32
	ConnectRetries    uint32
}

type Message struct {
	Src       string
	Data      []byte
	Type      payloads.PayloadType
	Encrypted bool
	Pid       []byte
	Reply     func([]byte)
}

type Client struct {
	config            ClientConfig
	account           *vault.Account
	publicKey         []byte
	curveSecretKey    *[sharedKeySize]byte
	Address           string
	addressID         []byte
	OnConnect         chan struct{}
	OnMessage         chan *Message
	OnBlock           chan *BlockInfo
	sigChainBlockHash string
	reconnectChan     chan struct{}
	responseChannels  map[string]chan *Message

	sync.RWMutex
	closed    bool
	conn      *websocket.Conn
	nodeInfo  *NodeInfo
	urlString string
}

type NodeInfo struct {
	Address   string `json:"addr"`
	PublicKey string `json:"pubkey"`
	Id        string `json:"id"`
}

type SetClientResult struct {
	NodeInfo          *NodeInfo `json:"node"`
	SigChainBlockHash string    `json:"sigChainBlockHash"`
}

type HeaderInfo struct {
	Version          uint32 `json:"version"`
	PrevBlockHash    string `json:"prevBlockHash"`
	TransactionsRoot string `json:"transactionsRoot"`
	StateRoot        string `json:"stateRoot"`
	Timestamp        int64  `json:"timestamp"`
	Height           uint32 `json:"height"`
	RandomBeacon     string `json:"randomBeacon"`
	WinnerHash       string `json:"winnerHash"`
	WinnerType       string `json:"winnerType"`
	SignerPk         string `json:"signerPk"`
	SignerId         string `json:"signerId"`
	Signature        string `json:"signature"`
	Hash             string `json:"hash"`
}

type ProgramInfo struct {
	Code      string `json:"code"`
	Parameter string `json:"parameter"`
}

type TxnInfo struct {
	TxType      string        `json:"txType"`
	PayloadData string        `json:"payloadData"`
	Nonce       uint64        `json:"nonce"`
	Fee         int64         `json:"fee"`
	Attributes  string        `json:"attributes"`
	Programs    []ProgramInfo `json:"programs"`
	Hash        string        `json:"hash"`
}

type BlockInfo struct {
	Header       HeaderInfo `json:"header"`
	Transactions []TxnInfo  `json:"transactions"`
	Size         int        `json:"size"`
	Hash         string     `json:"hash"`
}

func (c *Client) IsClosed() bool {
	c.RLock()
	defer c.RUnlock()
	return c.closed
}

func (c *Client) Close() {
	c.Lock()
	defer c.Unlock()
	if !c.closed {
		c.closed = true
		close(c.OnConnect)
		close(c.OnMessage)
		close(c.OnBlock)
		close(c.reconnectChan)
		c.conn.Close()
	}
}

func (c *Client) GetNodeInfo() *NodeInfo {
	c.RLock()
	defer c.RUnlock()
	return c.nodeInfo
}

func (c *Client) GetConn() *websocket.Conn {
	c.RLock()
	defer c.RUnlock()
	return c.conn
}

func (c *Client) computeSharedKey(remotePublicKey []byte) (*[sharedKeySize]byte, error) {
	if len(remotePublicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("public key length is %d, expecting %d", len(remotePublicKey), ed25519.PublicKeySize)
	}

	var pk [ed25519.PublicKeySize]byte
	copy(pk[:], remotePublicKey)
	curve25519PublicKey, ok := ed25519.PublicKeyToCurve25519PublicKey(&pk)
	if !ok {
		return nil, fmt.Errorf("converting public key %x to curve25519 public key failed", remotePublicKey)
	}

	var sharedKey [sharedKeySize]byte
	box.Precompute(&sharedKey, curve25519PublicKey, c.curveSecretKey)
	return &sharedKey, nil
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

			sharedKey, err := c.computeSharedKey(destPubkey)
			if err != nil {
				return nil, err
			}

			encryptedKey, keyNonce, err := encrypt(key[:], sharedKey)
			if err != nil {
				return nil, err
			}

			nonce := append(keyNonce, msgNonce...)

			msgs[i], err = proto.Marshal(&payloads.Message{
				encrypted,
				true,
				nonce,
				encryptedKey,
			})
			if err != nil {
				return nil, err
			}
		}

		return msgs, nil
	} else {
		_, destPubkey, _, err := address.ParseClientAddress(dests[0])
		if err != nil {
			return nil, err
		}

		sharedKey, err := c.computeSharedKey(destPubkey)
		if err != nil {
			return nil, err
		}

		encrypted, nonce, err := encrypt(rawPayload, sharedKey)
		if err != nil {
			return nil, err
		}

		data, err := proto.Marshal(&payloads.Message{
			encrypted,
			true,
			nonce,
			nil,
		})
		if err != nil {
			return nil, err
		}
		return [][]byte{data}, nil
	}
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

		sharedKey, err := c.computeSharedKey(srcPubkey)
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

		sharedKey, err := c.computeSharedKey(srcPubkey)
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
				var nodeInfo NodeInfo
				if err := json.Unmarshal(*msg["Result"], &nodeInfo); err != nil {
					return err
				}
				go func() {
					err := c.connectToNode(&nodeInfo)
					if err != nil {
						c.reconnect()
					}
				}()
			} else if action == "setClient" {
				c.Close()
			}
			return errors.New(common.ErrMessage[errCode])
		}
		switch action {
		case "setClient":
			var setClientResult SetClientResult
			if err := json.Unmarshal(*msg["Result"], &setClientResult); err != nil {
				return err
			}
			c.sigChainBlockHash = setClientResult.SigChainBlockHash
			select {
			case c.OnConnect <- struct{}{}:
			default:
			}
		case "updateSigChainBlockHash":
			var sigChainBlockHash string
			if err := json.Unmarshal(*msg["Result"], &sigChainBlockHash); err != nil {
				return err
			}
			c.sigChainBlockHash = sigChainBlockHash
		case "sendRawBlock":
			var blockInfo BlockInfo
			if err := json.Unmarshal(*msg["Result"], &blockInfo); err != nil {
				return err
			}
			select {
			case c.OnBlock <- &blockInfo:
			default:
				log.Println("Block chan full, discarding block")
			}
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
			case payloads.TEXT:
				fallthrough
			case payloads.BINARY:
				msg = &Message{
					Src:       inboundMsg.Src,
					Data:      data,
					Type:      payload.Type,
					Encrypted: payloadMsg.Encrypted,
					Pid:       payload.Pid,
				}
			}

			if len(payload.ReplyToPid) > 0 {
				pidString := string(payload.ReplyToPid)
				if responseChannel, ok := c.responseChannels[pidString]; ok {
					responseChannel <- msg
				}
				return nil
			}

			msg.Reply = func(response []byte) {
				pid := payload.Pid
				var payload *payloads.Payload
				var err error
				if response == nil {
					payload, err = newAckPayload(pid)
				} else {
					payload, err = newBinaryPayload(response, pid)
				}
				if err != nil {
					log.Println("Problem creating response to PID " + hex.EncodeToString(pid))
				}
				if err := c.send([]string{inboundMsg.Src}, payload, payloadMsg.Encrypted); err != nil {
					log.Println("Problem sending response to PID " + hex.EncodeToString(pid))
				}
			}

			select {
			case c.OnMessage <- msg:
			default:
				log.Println("Message chan full, discarding msg")
			}
		}
	}

	return nil
}

func (c *Client) connectToNode(nodeInfo *NodeInfo) error {
	urlString := (&url.URL{Scheme: "ws", Host: nodeInfo.Address}).String()
	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = handshakeTimeout

	conn, _, err := dialer.Dial(urlString, nil)
	if err != nil {
		return err
	}

	c.Lock()
	prevConn := c.conn
	c.conn = conn
	c.nodeInfo = nodeInfo
	c.urlString = urlString
	c.Unlock()

	if prevConn != nil {
		prevConn.Close()
	}

	go func() {
		req := make(map[string]interface{})
		req["Action"] = "setClient"
		req["Addr"] = c.Address

		c.Lock()
		err := conn.WriteJSON(req)
		c.Unlock()
		if err != nil {
			log.Println(err)
			c.reconnect()
			return
		}
	}()

	go func() {
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
	retryInterval := c.config.ReconnectInterval
	for retry := 0; maxRetries < 0 || retry <= maxRetries; retry++ {
		if retry > 0 {
			log.Printf("Retry in %v...\n", retryInterval)
			time.Sleep(retryInterval)
			retryInterval *= 2
			if retryInterval > maxRetryInterval {
				retryInterval = maxRetryInterval
			}
		}

		var nodeInfo *NodeInfo
		err, _ := call(c.config.SeedRPCServerAddr, "getwsaddr", map[string]interface{}{"address": c.Address}, &nodeInfo)
		if err != nil {
			log.Println(err)
			continue
		}

		err = c.connectToNode(nodeInfo)
		if err != nil {
			log.Println(err)
			continue
		}

		return nil
	}

	return errors.New("max retry reached, connect failed")
}

func (c *Client) reconnect() {
	select {
	case c.reconnectChan <- struct{}{}:
	default:
	}
}

func (c *Client) handleReconnect() {
	for _ = range c.reconnectChan {
		if c.IsClosed() {
			return
		}

		log.Printf("Reconnect in %v...\n", c.config.ReconnectInterval)
		time.Sleep(c.config.ReconnectInterval)

		err := c.connect(-1)
		if err != nil {
			log.Println(err)
			c.Close()
			return
		}
	}
}

func addressToID(addr string) []byte {
	id := sha256.Sum256([]byte(addr))
	return id[:]
}

func defaultConfig() ClientConfig {
	return ClientConfig{
		SeedRPCServerAddr: seedRPCServerAddr,
		ReconnectInterval: defaultReconnectInterval,
		MaxHoldingSeconds: 0,
		MsgChanLen:        defaultMsgChanLen,
		BlockChanLen:      defaultBlockChanLen,
		ConnectRetries:    defaultConnectRetries,
	}
}

func getConfig(configs []ClientConfig) ClientConfig {
	var config ClientConfig
	if len(configs) == 0 {
		return defaultConfig()
	} else {
		config = configs[0]
		if config.SeedRPCServerAddr == "" {
			config.SeedRPCServerAddr = seedRPCServerAddr
		}
		if config.ReconnectInterval == 0 {
			config.ReconnectInterval = defaultReconnectInterval
		}
		if config.MsgChanLen == 0 {
			config.MsgChanLen = defaultMsgChanLen
		}
		if config.BlockChanLen == 0 {
			config.BlockChanLen = defaultBlockChanLen
		}
	}
	return config
}

func NewClient(account *vault.Account, identifier string, configs ...ClientConfig) (*Client, error) {
	config := getConfig(configs)

	pk := account.PubKey().EncodePoint()

	var sk [ed25519.PrivateKeySize]byte
	copy(sk[:], account.PrivKey())
	curveSecretKey := ed25519.PrivateKeyToCurve25519PrivateKey(&sk)

	addr := address.MakeAddressString(pk, identifier)
	c := Client{
		config:           config,
		account:          account,
		publicKey:        pk,
		curveSecretKey:   curveSecretKey,
		Address:          addr,
		addressID:        addressToID(addr),
		OnConnect:        make(chan struct{}, 1),
		OnMessage:        make(chan *Message, config.MsgChanLen),
		OnBlock:          make(chan *BlockInfo, config.BlockChanLen),
		reconnectChan:    make(chan struct{}, 0),
		responseChannels: make(map[string]chan *Message),
	}

	go c.handleReconnect()

	err := c.connect(int(c.config.ConnectRetries))
	if err != nil {
		return nil, err
	}

	return &c, nil
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
	clientMsgData, err := proto.Marshal(clientMsg)
	if err != nil {
		return err
	}

	c.Lock()
	err = c.conn.WriteMessage(websocket.BinaryMessage, clientMsgData)
	c.Unlock()
	if err != nil {
		c.reconnect()
	}
	return err
}

func newBinaryPayload(data []byte, replyToPid []byte) (*payloads.Payload, error) {
	pid := make([]byte, 8)
	if _, err := rand.Read(pid); err != nil {
		return nil, err
	}

	return &payloads.Payload{
		Type:       payloads.BINARY,
		Pid:        pid,
		Data:       data,
		ReplyToPid: replyToPid,
	}, nil
}

func newAckPayload(replyToPid []byte) (*payloads.Payload, error) {
	pid := make([]byte, 8)
	if _, err := rand.Read(pid); err != nil {
		return nil, err
	}

	return &payloads.Payload{
		Type:       payloads.ACK,
		Pid:        pid,
		ReplyToPid: replyToPid,
	}, nil
}

func (c *Client) Send(dests []string, data []byte, encrypted bool, MaxHoldingSeconds ...uint32) (*Message, error) {
	payload, err := newBinaryPayload(data, nil)
	if err != nil {
		return nil, err
	}
	pidString := string(payload.Pid)
	responseChannel := make(chan *Message, 1)
	c.responseChannels[pidString] = responseChannel
	if err := c.send(dests, payload, encrypted, MaxHoldingSeconds...); err != nil {
		return nil, err
	}
	msg := <-responseChannel
	return msg, nil
}

func (c *Client) send(dests []string, payload *payloads.Payload, encrypted bool, MaxHoldingSeconds ...uint32) error {
	var outboundMsg *pb.OutboundMessage
	var payloadMsgs [][]byte

	if encrypted {
		var err error
		payloadMsgs, err = c.encryptPayload(payload, dests)
		if err != nil {
			return err
		}
		outboundMsg = &pb.OutboundMessage{
			Payloads: payloadMsgs,
			Dests:    dests,
		}
	} else {
		payloadData, err := proto.Marshal(payload)
		if err != nil {
			return err
		}

		data, err := proto.Marshal(&payloads.Message{
			payloadData,
			false,
			nil,
			nil,
		})
		if err != nil {
			return err
		}
		payloadMsgs = [][]byte{data}
		outboundMsg = &pb.OutboundMessage{
			Payload: data,
			Dests:   dests,
		}
	}

	if len(MaxHoldingSeconds) == 0 {
		outboundMsg.MaxHoldingSeconds = c.config.MaxHoldingSeconds
	} else {
		outboundMsg.MaxHoldingSeconds = MaxHoldingSeconds[0]
	}

	nodePk, err := hex.DecodeString(c.nodeInfo.PublicKey)
	if err != nil {
		return err
	}
	sigChainElem := &pb.SigChainElem{
		NextPubkey: nodePk,
	}
	buff := bytes.NewBuffer(nil)
	if err := sigChainElem.SerializationUnsigned(buff); err != nil {
		return err
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
			return err
		}
		sigChain.BlockHash = sigChainBlockHash
		outboundMsg.BlockHash = sigChainBlockHash
	}

	var signatures [][]byte

	for i, dest := range dests {
		destId, destPubKey, _, err := address.ParseClientAddress(dest)
		if err != nil {
			return err
		}
		sigChain.DestId = destId
		sigChain.DestPubkey = destPubKey
		if len(payloadMsgs) > 1 {
			sigChain.DataSize = uint32(len(payloadMsgs[i]))
		} else {
			sigChain.DataSize = uint32(len(payloadMsgs[0]))
		}
		buff := bytes.NewBuffer(nil)
		if err := sigChain.SerializationMetadata(buff); err != nil {
			return err
		}
		digest := sha256.Sum256(buff.Bytes())
		digest = sha256.Sum256(append(digest[:], sigChainElemSerialized...))
		signature, err := crypto.Sign(c.account.PrivateKey, digest[:])
		if err != nil {
			return err
		}
		signatures = append(signatures, signature)
	}

	outboundMsg.Signatures = signatures
	outboundMsg.Nonce = nonce

	outboundMsgData, err := proto.Marshal(outboundMsg)
	if err != nil {
		return err
	}

	clientMsg := &pb.ClientMessage{
		MessageType: pb.OUTBOUND_MESSAGE,
	}

	if len(payloadMsgs) > 1 {
		clientMsg.CompressionType = pb.COMPRESSION_ZLIB

		var b bytes.Buffer
		w := zlib.NewWriter(&b)
		_, err = w.Write(outboundMsgData)
		if err != nil {
			return err
		}
		err = w.Close()
		if err != nil {
			return err
		}
		clientMsg.Message = b.Bytes()
	} else {
		clientMsg.CompressionType = pb.COMPRESSION_NONE
		clientMsg.Message = outboundMsgData
	}

	clientMsgData, err := proto.Marshal(clientMsg)
	if err != nil {
		return err
	}

	c.Lock()
	err = c.conn.WriteMessage(websocket.BinaryMessage, clientMsgData)
	c.Unlock()
	if err != nil {
		c.reconnect()
	}
	return err
}

func (c *Client) Publish(topic string, offset, limit uint32, txPool bool, data []byte, encrypted bool, MaxHoldingSeconds ...uint32) error {
	subscribers, subscribersInTxPool, err := getSubscribers(c.config.SeedRPCServerAddr, topic, offset, limit, false, txPool)
	dests := make([]string, 0, len(subscribers)+len(subscribersInTxPool))
	for subscriber, _ := range subscribers {
		dests = append(dests, subscriber)
	}
	for subscriber, _ := range subscribersInTxPool {
		dests = append(dests, subscriber)
	}
	if err != nil {
		return err
	}
	payload, err := newBinaryPayload(data, nil)
	if err != nil {
		return err
	}
	return c.send(dests, payload, encrypted, MaxHoldingSeconds...)
}
