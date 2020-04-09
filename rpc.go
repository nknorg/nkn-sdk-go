package nkn

import (
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/nknorg/nkn/api/httpjson/client"
	"github.com/nknorg/nkn/transaction"
)

// RPCConfigInterface is the config interface for making rpc call. ClientConfig,
// WalletConfig and RPCConfig all implement this interface and thus can be used
// directly.
type RPCConfigInterface interface {
	GetRandomSeedRPCServerAddr() string
}

// Node struct contains the information of the node that a client connects to.
type Node struct {
	Address   string `json:"addr"`
	PublicKey string `json:"pubkey"`
	ID        string `json:"id"`
}

// Subscription contains the information of a subscriber to a topic.
type Subscription struct {
	Meta      string `json:"meta"`
	ExpiresAt int32  `json:"expiresAt"` // Changed to signed int for gomobile compatibility
}

// Registrant contains the information of a name registrant
type Registrant struct {
	Registrant string `json:"registrant"`
	ExpiresAt  int32  `json:"expiresAt"` // Changed to signed int for gomobile compatibility
}

type balance struct {
	Amount string `json:"amount"`
}

type nonce struct {
	Nonce         uint64 `json:"nonce"`
	NonceInTxPool uint64 `json:"nonceInTxPool"`
}

type errResp struct {
	Code    int32
	Message string
	Data    string
}

// GetWsAddr RPC gets the node that a client address should connect to using ws.
func GetWsAddr(clientAddr string, config RPCConfigInterface) (*Node, error) {
	node := &Node{}
	err := RPCCall("getwsaddr", map[string]interface{}{"address": clientAddr}, node, config)
	if err != nil {
		return nil, err
	}
	return node, nil
}

// GetWssAddr RPC gets the node that a client address should connect to using
// wss.
func GetWssAddr(clientAddr string, config RPCConfigInterface) (*Node, error) {
	node := &Node{}
	err := RPCCall("getwssaddr", map[string]interface{}{"address": clientAddr}, node, config)
	if err != nil {
		return nil, err
	}
	return node, nil
}

// GetRegistrant RPC gets the registrant of a name.
func GetRegistrant(name string, config RPCConfigInterface) (*Registrant, error) {
	registrant := &Registrant{}
	err := RPCCall("getregistrant", map[string]interface{}{"name": name}, registrant, config)
	if err != nil {
		return nil, err
	}
	return registrant, nil
}

// SendRawTransaction RPC sends a signed transaction to chain and returns txn
// hash hex string.
func SendRawTransaction(txn *transaction.Transaction, config RPCConfigInterface) (string, error) {
	var txnHash string
	err := RPCCall("sendrawtransaction", map[string]interface{}{"tx": hex.EncodeToString(txn.ToArray())}, &txnHash, config)
	if err != nil {
		return "", err
	}
	return txnHash, nil
}

// GetNonce RPC gets the next nonce to use of an address. If txPool is false,
// result only counts transactions in ledger; if txPool is true, transactions in
// txPool are also counted.
//
// Nonce is changed to signed int for gomobile compatibility.
func GetNonce(address string, txPool bool, config RPCConfigInterface) (int64, error) {
	n := &nonce{}
	err := RPCCall("getnoncebyaddr", map[string]interface{}{"address": address}, n, config)
	if err != nil {
		return 0, err
	}
	if txPool && n.NonceInTxPool > n.Nonce {
		return int64(n.NonceInTxPool), nil
	}
	return int64(n.Nonce), nil
}

// GetBalance RPC returns the balance of a wallet address.
func GetBalance(address string, config RPCConfigInterface) (*Amount, error) {
	balance := &balance{}
	err := RPCCall("getbalancebyaddr", map[string]interface{}{"address": address}, balance, config)
	if err != nil {
		return nil, err
	}
	return NewAmount(balance.Amount)
}

// GetHeight RPC returns the latest block height.
func GetHeight(config RPCConfigInterface) (int32, error) {
	var height uint32
	err := RPCCall("getlatestblockheight", map[string]interface{}{}, &height, config)
	if err != nil {
		return 0, err
	}
	return int32(height), nil
}

// GetSubscribers gets the subscribers of a topic with a offset and max number
// of results (limit). If meta is true, results contain each subscriber's
// metadata. If txPool is true, results contain subscribers in txPool. Enabling
// this will get subscribers sooner after they send subscribe transactions, but
// might affect the correctness of subscribers because transactions in txpool is
// not guaranteed to be packed into a block.
//
// Offset and limit are changed to signed int for gomobile compatibility
func GetSubscribers(topic string, offset, limit int, meta, txPool bool, config RPCConfigInterface) (*Subscribers, error) {
	var result map[string]interface{}
	err := RPCCall("getsubscribers", map[string]interface{}{
		"topic":  topic,
		"offset": offset,
		"limit":  limit,
		"meta":   meta,
		"txPool": txPool,
	}, &result, config)
	if err != nil {
		return nil, err
	}

	subscribers := make(map[string]string)
	subscribersInTxPool := make(map[string]string)

	if meta {
		for subscriber, meta := range result["subscribers"].(map[string]interface{}) {
			subscribers[subscriber] = meta.(string)
		}
		if txPool {
			for subscriber, meta := range result["subscribersInTxPool"].(map[string]interface{}) {
				subscribersInTxPool[subscriber] = meta.(string)
			}
		}
	} else {
		for _, subscriber := range result["subscribers"].([]interface{}) {
			subscribers[subscriber.(string)] = ""
		}
		if txPool {
			for _, subscriber := range result["subscribersInTxPool"].([]interface{}) {
				subscribersInTxPool[subscriber.(string)] = ""
			}
		}
	}

	return &Subscribers{
		Subscribers:         NewStringMap(subscribers),
		SubscribersInTxPool: NewStringMap(subscribersInTxPool),
	}, nil
}

// GetSubscription RPC gets the subscription details of a subscriber in a topic.
func GetSubscription(topic string, subscriber string, config RPCConfigInterface) (*Subscription, error) {
	subscription := &Subscription{}
	err := RPCCall("getsubscription", map[string]interface{}{
		"topic":      topic,
		"subscriber": subscriber,
	}, subscription, config)
	if err != nil {
		return nil, err
	}
	return subscription, nil
}

// GetSubscribersCount RPC returns the number of subscribers of a topic (not
// including txPool).
//
// Count is changed to signed int for gomobile compatibility
func GetSubscribersCount(topic string, config RPCConfigInterface) (int, error) {
	var count int
	err := RPCCall("getsubscriberscount", map[string]interface{}{"topic": topic}, &count, config)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// RPCCall makes a RPC call and put results to result passed in.
func RPCCall(action string, params map[string]interface{}, result interface{}, config RPCConfigInterface) error {
	if config == nil {
		config = GetDefaultRPCConfig()
	}
	data, err := client.Call(config.GetRandomSeedRPCServerAddr(), action, 0, params)
	if err != nil {
		return &errorWithCode{err: err, code: -1}
	}
	resp := make(map[string]*json.RawMessage)
	err = json.Unmarshal(data, &resp)
	if err != nil {
		return &errorWithCode{err: err, code: -1}
	}
	if resp["error"] != nil {
		er := &errResp{}
		err := json.Unmarshal(*resp["error"], er)
		if err != nil {
			return &errorWithCode{err: err, code: -1}
		}
		code := er.Code
		if code < 0 {
			code = -1
		}
		msg := er.Message
		if len(er.Data) > 0 {
			msg += ": " + er.Data
		}
		return &errorWithCode{err: errors.New(msg), code: code}
	}

	err = json.Unmarshal(*resp["result"], result)
	if err != nil {
		return &errorWithCode{err: err, code: 0}
	}
	return nil
}
