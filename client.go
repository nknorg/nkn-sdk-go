package nkn_sdk_go

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/url"
	"sync"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/gorilla/websocket"
	"github.com/nknorg/nkn/api/common"
	"github.com/nknorg/nkn/crypto"
	"github.com/nknorg/nkn/pb"
	"github.com/nknorg/nkn/util/address"
	"github.com/nknorg/nkn/vault"
	"github.com/pkg/errors"
)

const (
	defaultReconnectInterval = time.Second
	defaultMsgChanLen        = 1024
	defaultBlockChanLen      = 1
	defaultConnectRetries    = 3
	handshakeTimeout         = 5 * time.Second
	maxRetryInterval         = time.Minute
)

type ClientConfig struct {
	SeedRPCServerAddr string
	ReconnectInterval time.Duration
	MaxHoldingSeconds uint32
	MsgChanLen        uint32
	BlockChanLen      uint32
	ConnectRetries    uint32
}

type Client struct {
	config            ClientConfig
	account           *vault.Account
	publicKey         []byte
	Address           string
	addressID         []byte
	OnConnect         chan struct{}
	OnMessage         chan *pb.InboundMessage
	OnBlock           chan *BlockInfo
	sigChainBlockHash string
	reconnectChan     chan struct{}

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
			select {
			case c.OnMessage <- inboundMsg:
			default:
				log.Println("Message chan full, discarding msg")
			}
			if len(inboundMsg.PrevSignature) > 0 {
				go func() {
					if err := c.sendReceipt(inboundMsg.PrevSignature); err != nil {
						log.Println(err)
					}
				}()
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
			if err != nil {
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

func NewClient(account *vault.Account, identifier string, configs ...ClientConfig) (*Client, error) {
	var config ClientConfig
	if len(configs) == 0 {
		config = ClientConfig{
			SeedRPCServerAddr: seedRPCServerAddr,
			ReconnectInterval: defaultReconnectInterval,
			MaxHoldingSeconds: 0,
			MsgChanLen:        defaultMsgChanLen,
			BlockChanLen:      defaultBlockChanLen,
			ConnectRetries:    defaultConnectRetries,
		}
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

	pk := account.PubKey().EncodePoint()
	addr := address.MakeAddressString(pk, identifier)
	c := Client{
		config:        config,
		account:       account,
		publicKey:     pk,
		Address:       addr,
		addressID:     addressToID(addr),
		OnConnect:     make(chan struct{}, 1),
		OnMessage:     make(chan *pb.InboundMessage, config.MsgChanLen),
		OnBlock:       make(chan *BlockInfo, config.BlockChanLen),
		reconnectChan: make(chan struct{}, 0),
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
		MessageType: pb.RECEIPT,
		Message:     receiptData,
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

func (c *Client) Send(dests []string, payload []byte, MaxHoldingSeconds ...uint32) error {
	outboundMsg := &pb.OutboundMessage{
		Payload: payload,
		Dests:   dests,
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
		DataSize:  uint32(len(payload)),
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

	for _, dest := range dests {
		destId, destPubKey, _, err := address.ParseClientAddress(dest)
		if err != nil {
			return err
		}
		sigChain.DestId = destId
		sigChain.DestPubkey = destPubKey

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
		Message:     outboundMsgData,
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

func (c *Client) Publish(topic string, offset, limit uint32, txPool bool, payload []byte, MaxHoldingSeconds ...uint32) error {
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
	return c.Send(dests, payload, MaxHoldingSeconds...)
}
