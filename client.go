package nkn_sdk_go

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/url"
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

var reconnectInterval time.Duration = 1

type ClientConfig struct {
	SeedRPCServerAddr string
	ReconnectInterval time.Duration
	MaxHoldingSeconds uint32
}

type Client struct {
	config    ClientConfig
	account   *vault.Account
	publicKey []byte
	Address   string
	addressId []byte
	urlString string
	conn      *websocket.Conn
	closed    bool
	OnConnect chan struct{}
	OnMessage chan *pb.InboundMessage
	OnBlock   chan *BlockInfo

	nodeInfo          *NodeInfo
	sigChainBlockHash string
}

type NodeInfo struct {
	Address   string `json:"addr"`
	PublicKey []byte `json:"pubkey"`
	Id        []byte `json:"id"`
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

func (c *Client) connect() error {
	conn, err := func() (*websocket.Conn, error) {
		var nodeInfo *NodeInfo
		err, _ := call(c.config.SeedRPCServerAddr, "getwsaddr", map[string]interface{}{"address": c.Address}, &nodeInfo)
		if err != nil {
			return nil, err
		}
		c.nodeInfo = nodeInfo
		c.urlString = (&url.URL{Scheme: "ws", Host: nodeInfo.Address}).String()

		conn, _, err := websocket.DefaultDialer.Dial(c.urlString, nil)
		return conn, err
	}()

	if err != nil && !c.closed {
		log.Println(err)

		time.Sleep(c.config.ReconnectInterval * time.Second)

		return c.connect()
	}

	c.conn = conn
	c.OnConnect = make(chan struct{})
	c.OnMessage = make(chan *pb.InboundMessage)
	c.OnBlock = make(chan *BlockInfo)

	go func() {
		defer func() {
			close(c.OnConnect)
			close(c.OnMessage)
			close(c.OnBlock)

			_ = c.conn.Close()
		}()

		err := func() error {
			req := make(map[string]interface{})
			req["Action"] = "setClient"
			req["Addr"] = c.Address
			if err := conn.WriteJSON(req); err != nil {
				return err
			}

			for {
				msgType, data, err := conn.ReadMessage()
				if err != nil {
					if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
						return err
					}
					return nil
				}

				switch msgType {
				case websocket.TextMessage:
					msg := make(map[string]*json.RawMessage)
					if err := json.Unmarshal(data, &msg); err != nil {
						return err
					}
					var errCode common.ErrCode
					if err := json.Unmarshal(*msg["Error"], &errCode); err != nil {
						return err
					}
					if errCode != common.SUCCESS {
						return errors.New(common.ErrMessage[errCode])
					}
					var action string
					if err := json.Unmarshal(*msg["Action"], &action); err != nil {
						return err
					}
					switch action {
					case "setClient":
						var setClientResult SetClientResult
						if err := json.Unmarshal(*msg["Result"], &setClientResult); err != nil {
							return err
						}
						c.sigChainBlockHash = setClientResult.SigChainBlockHash
						c.OnConnect <- struct{}{}
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
						c.OnBlock <- &blockInfo
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
						c.OnMessage <- inboundMsg
						go func() {
							if err := c.sendReceipt(inboundMsg.PrevSignature); err != nil {
								log.Println(err)
							}
						}()
					}
				}
			}
		}()

		if err != nil {
			log.Println(err)
		}

		if !c.closed {
			defer c.connect()
		}
	}()

	return nil
}

func NewClient(account *vault.Account, identifier string, config ...ClientConfig) (*Client, error) {
	var _config ClientConfig
	if len(config) == 0 {
		_config = ClientConfig{seedRPCServerAddr, reconnectInterval, 0}
	} else {
		_config = config[0]
		if _config.SeedRPCServerAddr == "" {
			_config.SeedRPCServerAddr = seedRPCServerAddr
		}
		if _config.ReconnectInterval == 0 {
			_config.ReconnectInterval = reconnectInterval
		}
	}
	c := Client{
		config: _config,
		account: account,
		publicKey: account.PubKey().EncodePoint(),
	}
	c.Address = address.MakeAddressString(c.publicKey, identifier)
	addressId := sha256.Sum256([]byte(c.Address))
	c.addressId = addressId[:]

	err := c.connect()
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
	return c.conn.WriteMessage(websocket.BinaryMessage, clientMsgData)
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

	sigChainElem := &pb.SigChainElem{
		NextPubkey: c.nodeInfo.PublicKey,
	}
	buff := bytes.NewBuffer(nil)
	if err := sigChainElem.SerializationUnsigned(buff); err != nil {
		return err
	}
	sigChainElemSerialized := buff.Bytes()

	nonce := randUint32()

	sigChain := pb.SigChain{
		Nonce:      nonce,
		DataSize:   uint32(len(payload)),
		SrcId:      c.addressId,
		SrcPubkey:  c.publicKey,
		Elems:      []*pb.SigChainElem{sigChainElem},
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

	return c.conn.WriteMessage(websocket.BinaryMessage, clientMsgData)
}

func (c *Client) Publish(topic string, bucket uint32, payload []byte, MaxHoldingSeconds ...uint32) error {
	subscribers, err := getSubscribers(c.config.SeedRPCServerAddr, topic, bucket)
	dests := make([]string, 0, len(subscribers))
	for subscriber, _ := range subscribers {
		dests = append(dests, subscriber)
	}
	if err != nil {
		return err
	}
	return c.Send(dests, payload, MaxHoldingSeconds...)
}

func (c *Client) Close() {
	if !c.closed {
		c.closed = true
		_ = c.conn.Close()
	}
}
