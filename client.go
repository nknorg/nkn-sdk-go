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
	"github.com/patrickmn/go-cache"
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
	nonceSize     = 24
	sharedKeySize = 32
)

type Message struct {
	Src       string
	Data      []byte
	Type      payloads.PayloadType
	Encrypted bool
	Pid       []byte
	Reply     func([]byte) error
}

type Client struct {
	config            *ClientConfig
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
	responseChannels  *cache.Cache

	sync.RWMutex
	closed     bool
	conn       *websocket.Conn
	nodeInfo   *NodeInfo
	urlString  string
	sharedKeys map[string]*[sharedKeySize]byte
}

type clientInterface interface {
	getConfig() *ClientConfig
	send(dests []string, payload *payloads.Payload, encrypted bool, maxHoldingSeconds int32) error
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

func (c *Client) getOrComputeSharedKey(remotePublicKey []byte) (*[sharedKeySize]byte, error) {
	c.RLock()
	sharedKey, ok := c.sharedKeys[string(remotePublicKey)]
	c.RUnlock()
	if ok && sharedKey != nil {
		return sharedKey, nil
	}

	if len(remotePublicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("public key length is %d, expecting %d", len(remotePublicKey), ed25519.PublicKeySize)
	}

	var pk [ed25519.PublicKeySize]byte
	copy(pk[:], remotePublicKey)
	curve25519PublicKey, ok := ed25519.PublicKeyToCurve25519PublicKey(&pk)
	if !ok {
		return nil, fmt.Errorf("converting public key %x to curve25519 public key failed", remotePublicKey)
	}

	sharedKey = new([sharedKeySize]byte)
	box.Precompute(sharedKey, curve25519PublicKey, c.curveSecretKey)

	c.Lock()
	c.sharedKeys[string(remotePublicKey)] = sharedKey
	c.Unlock()

	return sharedKey, nil
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
	} else {
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

			c.RLock()
			defer c.RUnlock()
			if c.closed {
				return nil
			}

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

			c.RLock()
			defer c.RUnlock()
			if c.closed {
				return nil
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
			case payloads.TEXT, payloads.SESSION:
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
				respChan, ok := c.responseChannels.Get(pidString)
				if ok {
					c.responseChannels.Delete(pidString)
					select {
					case respChan.(chan *Message) <- msg:
					default:
					}
				}
				return nil
			}

			if msg == nil {
				return nil
			}

			msg.Reply = func(response []byte) error {
				pid := payload.Pid
				var payload *payloads.Payload
				var err error
				if response == nil {
					payload, err = newAckPayload(pid)
				} else {
					payload, err = newBinaryPayload(response, pid, false)
				}
				if err != nil {
					return err
				}
				if err := c.send([]string{inboundMsg.Src}, payload, payloadMsg.Encrypted, 0); err != nil {
					return err
				}
				return nil
			}

			c.RLock()
			defer c.RUnlock()
			if c.closed {
				return nil
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
	dialer.HandshakeTimeout = c.config.WsHandshakeTimeout

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
	retryInterval := c.config.MinReconnectInterval
	for retry := 1; maxRetries == 0 || retry <= maxRetries; retry++ {
		if retry > 1 {
			log.Printf("Retry in %v...\n", retryInterval)
			time.Sleep(retryInterval)
			retryInterval *= 2
			if retryInterval > c.config.MaxReconnectInterval {
				retryInterval = c.config.MaxReconnectInterval
			}
		}

		var nodeInfo *NodeInfo
		err, _ := call(c.config.GetRandomSeedRPCServerAddr(), "getwsaddr", map[string]interface{}{"address": c.Address}, &nodeInfo)
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
	if c.IsClosed() {
		return
	}
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

		log.Printf("Reconnect in %v...\n", c.config.MinReconnectInterval)
		time.Sleep(c.config.MinReconnectInterval)

		err := c.connect(0)
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

func NewClient(account *vault.Account, identifier string, configs ...ClientConfig) (*Client, error) {
	config, err := MergeClientConfig(configs)
	if err != nil {
		return nil, err
	}

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
		responseChannels: cache.New(config.MsgCacheExpiration, config.MsgCacheExpiration),
		sharedKeys:       make(map[string]*[sharedKeySize]byte),
	}

	go c.handleReconnect()

	err = c.connect(int(c.config.ConnectRetries))
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

func newBinaryPayload(data []byte, replyToPid []byte, noAck bool) (*payloads.Payload, error) {
	pid := make([]byte, 8)
	if _, err := rand.Read(pid); err != nil {
		return nil, err
	}

	return &payloads.Payload{
		Type:       payloads.BINARY,
		Pid:        pid,
		Data:       data,
		ReplyToPid: replyToPid,
		NoAck:      noAck,
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

func (c *Client) Send(dests []string, data []byte, configs ...*MessageConfig) (chan *Message, error) {
	config, err := MergeMessageConfig(&c.config.MessageConfig, configs)
	if err != nil {
		return nil, err
	}

	payload, err := newBinaryPayload(data, nil, config.NoAck)
	if err != nil {
		return nil, err
	}

	if err := c.send(dests, payload, !config.Unencrypted, config.MaxHoldingSeconds); err != nil {
		return nil, err
	}

	pidString := string(payload.Pid)
	respChan := make(chan *Message, 1)
	c.responseChannels.Add(pidString, respChan, cache.DefaultExpiration)

	return respChan, nil
}

func (c *Client) send(dests []string, payload *payloads.Payload, encrypted bool, maxHoldingSeconds int32) error {
	if maxHoldingSeconds < 0 {
		maxHoldingSeconds = 0
	}

	var payloadMsgs [][]byte
	outboundMsg := &pb.OutboundMessage{
		Dests:             dests,
		MaxHoldingSeconds: uint32(maxHoldingSeconds),
	}

	if encrypted {
		var err error
		payloadMsgs, err = c.encryptPayload(payload, dests)
		if err != nil {
			return err
		}
		outboundMsg.Payloads = payloadMsgs
	} else {
		payloadData, err := proto.Marshal(payload)
		if err != nil {
			return err
		}
		data, err := proto.Marshal(&payloads.Message{
			Payload:      payloadData,
			Encrypted:    false,
			Nonce:        nil,
			EncryptedKey: nil,
		})
		if err != nil {
			return err
		}
		payloadMsgs = [][]byte{data}
		outboundMsg.Payload = data
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
		destID, destPubKey, _, err := address.ParseClientAddress(dest)
		if err != nil {
			return err
		}
		sigChain.DestId = destID
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

func publish(c clientInterface, topic string, data []byte, configs ...*MessageConfig) error {
	config, err := MergeMessageConfig(&c.getConfig().MessageConfig, configs)
	if err != nil {
		return err
	}

	subscribers, subscribersInTxPool, err := getSubscribers(c.getConfig().GetRandomSeedRPCServerAddr(), topic, uint32(config.Offset), uint32(config.Limit), false, config.TxPool)
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

	payload, err := newBinaryPayload(data, nil, true)
	if err != nil {
		return err
	}

	return c.send(dests, payload, !config.Unencrypted, config.MaxHoldingSeconds)
}

func (c *Client) Publish(topic string, data []byte, configs ...*MessageConfig) error {
	return publish(c, topic, data, configs...)
}

func (c *Client) SetWriteDeadline(deadline time.Time) error {
	c.Lock()
	defer c.Unlock()
	if c.conn == nil {
		return errors.New("nil websocker connection")
	}
	return c.conn.SetWriteDeadline(deadline)
}

func (c *Client) getConfig() *ClientConfig {
	return c.config
}
