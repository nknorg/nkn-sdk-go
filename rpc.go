package nkn

import (
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/nknorg/nkn/v2/api/httpjson/client"
	"github.com/nknorg/nkn/v2/common"
	"github.com/nknorg/nkn/v2/program"
	"github.com/nknorg/nkn/v2/transaction"
	nknConfig "github.com/nknorg/nkn/v2/util/config"
)

// Signer is the interface that can sign transactions.
type signer interface {
	PubKey() []byte
	SignTransaction(tx *transaction.Transaction) error
}

// RPCClient is the RPC client interface that implements most RPC methods
// (except a few ones that is supposed to call with seed node only).
type rpcClient interface {
	GetNonce(txPool bool) (int64, error)
	GetNonceByAddress(address string, txPool bool) (int64, error)
	Balance() (*Amount, error)
	BalanceByAddress(address string) (*Amount, error)
	GetHeight() (int32, error)
	GetSubscribers(topic string, offset, limit int, meta, txPool bool) (*Subscribers, error)
	GetSubscription(topic string, subscriber string) (*Subscription, error)
	GetSubscribersCount(topic string) (int, error)
	GetRegistrant(name string) (*Registrant, error)
	SendRawTransaction(txn *transaction.Transaction) (string, error)
}

// SignerRPCClient is a RPCClient that can also sign transactions and made RPC
// requests that requires signatures.
type signerRPCClient interface {
	signer
	rpcClient
	Transfer(address, amount string, config *TransactionConfig) (string, error)
	RegisterName(name string, config *TransactionConfig) (string, error)
	TransferName(name string, recipientPubKey []byte, config *TransactionConfig) (string, error)
	DeleteName(name string, config *TransactionConfig) (string, error)
	Subscribe(identifier, topic string, duration int, meta string, config *TransactionConfig) (string, error)
	Unsubscribe(identifier, topic string, config *TransactionConfig) (string, error)
}

// RPCConfigInterface is the config interface for making rpc call. ClientConfig,
// WalletConfig and RPCConfig all implement this interface and thus can be used
// directly.
type RPCConfigInterface interface {
	GetRandomSeedRPCServerAddr() string
}

// Node struct contains the information of the node that a client connects to.
type Node struct {
	Addr    string `json:"addr"`
	RPCAddr string `json:"rpcAddr"`
	PubKey  string `json:"pubkey"`
	ID      string `json:"id"`
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

// RPCCall makes a RPC call and put results to result passed in.
func RPCCall(action string, params map[string]interface{}, result interface{}, config RPCConfigInterface) error {
	if config == nil {
		config = GetDefaultRPCConfig()
	}
	data, err := client.Call(config.GetRandomSeedRPCServerAddr(), action, 0, params)
	if err != nil {
		return &errorWithCode{err: err, code: errCodeNetworkError}
	}
	resp := make(map[string]*json.RawMessage)
	err = json.Unmarshal(data, &resp)
	if err != nil {
		return &errorWithCode{err: err, code: errCodeDecodeError}
	}
	if resp["error"] != nil {
		er := &errResp{}
		err := json.Unmarshal(*resp["error"], er)
		if err != nil {
			return &errorWithCode{err: err, code: errCodeDecodeError}
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
		return &errorWithCode{err: err, code: errCodeDecodeError}
	}
	return nil
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
	b, err := txn.Marshal()
	if err != nil {
		return "", err
	}
	var txnHash string
	err = RPCCall("sendrawtransaction", map[string]interface{}{"tx": hex.EncodeToString(b)}, &txnHash, config)
	if err != nil {
		return "", err
	}
	return txnHash, nil
}

// Transfer sends asset to a wallet address with a transaction fee. Amount is
// the string representation of the amount in unit of NKN to avoid precision
// loss. For example, "0.1" will be parsed as 0.1 NKN. The signerRPCClient can
// be a client, multiclient or wallet.
func Transfer(s signerRPCClient, address, amount string, config *TransactionConfig) (string, error) {
	config, err := MergeTransactionConfig(config)
	if err != nil {
		return "", err
	}

	sender, err := program.CreateProgramHash(s.PubKey())
	if err != nil {
		return "", err
	}

	amountFixed64, err := common.StringToFixed64(amount)
	if err != nil {
		return "", err
	}

	recipient, err := common.ToScriptHash(address)
	if err != nil {
		return "", err
	}

	fee, err := common.StringToFixed64(config.Fee)
	if err != nil {
		return "", err
	}

	nonce := config.Nonce
	if nonce == 0 {
		nonce, err = s.GetNonce(true)
		if err != nil {
			return "", err
		}
	}

	tx, err := transaction.NewTransferAssetTransaction(sender, recipient, uint64(nonce), amountFixed64, fee)
	if err != nil {
		return "", err
	}

	if len(config.Attributes) > 0 {
		tx.Transaction.UnsignedTx.Attributes = config.Attributes
	}

	if err := s.SignTransaction(tx); err != nil {
		return "", err
	}

	return s.SendRawTransaction(tx)
}

// RegisterName registers a name for this signer's public key at the cost of 10
// NKN with a given transaction fee. The name will be valid for 1,576,800 blocks
// (around 1 year). Register name currently owned by this pubkey will extend the
// duration of the name to current block height + 1,576,800. Registration will
// fail if the name is currently owned by another account. The signerRPCClient
// can be a client, multiclient or wallet.
func RegisterName(s signerRPCClient, name string, config *TransactionConfig) (string, error) {
	config, err := MergeTransactionConfig(config)
	if err != nil {
		return "", err
	}

	fee, err := common.StringToFixed64(config.Fee)
	if err != nil {
		return "", err
	}

	nonce := config.Nonce
	if nonce == 0 {
		nonce, err = s.GetNonce(true)
		if err != nil {
			return "", err
		}
	}

	tx, err := transaction.NewRegisterNameTransaction(s.PubKey(), name, uint64(nonce), nknConfig.MinNameRegistrationFee, fee)
	if err != nil {
		return "", err
	}

	if len(config.Attributes) > 0 {
		tx.Transaction.UnsignedTx.Attributes = config.Attributes
	}

	if err := s.SignTransaction(tx); err != nil {
		return "", err
	}

	return s.SendRawTransaction(tx)
}

// TransferName transfers a name owned by this signer's pubkey to another public
// key with a transaction fee. The expiration height of the name will not be
// changed. The signerRPCClient can be a client, multiclient or wallet.
func TransferName(s signerRPCClient, name string, recipientPubKey []byte, config *TransactionConfig) (string, error) {
	config, err := MergeTransactionConfig(config)
	if err != nil {
		return "", err
	}

	fee, err := common.StringToFixed64(config.Fee)
	if err != nil {
		return "", err
	}

	nonce := config.Nonce
	if nonce == 0 {
		nonce, err = s.GetNonce(true)
		if err != nil {
			return "", err
		}
	}

	tx, err := transaction.NewTransferNameTransaction(s.PubKey(), recipientPubKey, name, uint64(nonce), fee)
	if err != nil {
		return "", err
	}

	if len(config.Attributes) > 0 {
		tx.Transaction.UnsignedTx.Attributes = config.Attributes
	}

	if err := s.SignTransaction(tx); err != nil {
		return "", err
	}

	return s.SendRawTransaction(tx)
}

// DeleteName deletes a name owned by this signer's pubkey with a given
// transaction fee. The signerRPCClient can be a client, multiclient or wallet.
func DeleteName(s signerRPCClient, name string, config *TransactionConfig) (string, error) {
	config, err := MergeTransactionConfig(config)
	if err != nil {
		return "", err
	}

	fee, err := common.StringToFixed64(config.Fee)
	if err != nil {
		return "", err
	}

	nonce := config.Nonce
	if nonce == 0 {
		nonce, err = s.GetNonce(true)
		if err != nil {
			return "", err
		}
	}

	tx, err := transaction.NewDeleteNameTransaction(s.PubKey(), name, uint64(nonce), fee)
	if err != nil {
		return "", err
	}

	if len(config.Attributes) > 0 {
		tx.Transaction.UnsignedTx.Attributes = config.Attributes
	}

	if err := s.SignTransaction(tx); err != nil {
		return "", err
	}

	return s.SendRawTransaction(tx)
}

// Subscribe to a topic with an identifier for a number of blocks. Client using
// the same key pair and identifier will be able to receive messages from this
// topic. If this (identifier, public key) pair is already subscribed to this
// topic, the subscription expiration will be extended to current block height +
// duration. The signerRPCClient can be a client, multiclient or wallet.
//
// Duration is changed to signed int for gomobile compatibility.
func Subscribe(s signerRPCClient, identifier, topic string, duration int, meta string, config *TransactionConfig) (string, error) {
	config, err := MergeTransactionConfig(config)
	if err != nil {
		return "", err
	}

	fee, err := common.StringToFixed64(config.Fee)
	if err != nil {
		return "", err
	}

	nonce := config.Nonce
	if nonce == 0 {
		nonce, err = s.GetNonce(true)
		if err != nil {
			return "", err
		}
	}

	tx, err := transaction.NewSubscribeTransaction(
		s.PubKey(),
		identifier,
		topic,
		uint32(duration),
		meta,
		uint64(nonce),
		fee,
	)
	if err != nil {
		return "", err
	}

	if len(config.Attributes) > 0 {
		tx.Transaction.UnsignedTx.Attributes = config.Attributes
	}

	if err := s.SignTransaction(tx); err != nil {
		return "", err
	}

	return s.SendRawTransaction(tx)
}

// Unsubscribe from a topic for an identifier. Client using the same key pair
// and identifier will no longer receive messages from this topic. The
// signerRPCClient can be a client, multiclient or wallet.
func Unsubscribe(s signerRPCClient, identifier, topic string, config *TransactionConfig) (string, error) {
	config, err := MergeTransactionConfig(config)
	if err != nil {
		return "", err
	}

	fee, err := common.StringToFixed64(config.Fee)
	if err != nil {
		return "", err
	}

	nonce := config.Nonce
	if nonce == 0 {
		nonce, err = s.GetNonce(true)
		if err != nil {
			return "", err
		}
	}

	tx, err := transaction.NewUnsubscribeTransaction(
		s.PubKey(),
		identifier,
		topic,
		uint64(nonce),
		fee,
	)
	if err != nil {
		return "", err
	}

	if len(config.Attributes) > 0 {
		tx.Transaction.UnsignedTx.Attributes = config.Attributes
	}

	if err := s.SignTransaction(tx); err != nil {
		return "", err
	}

	return s.SendRawTransaction(tx)
}
