package nkn

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/nknorg/nkn/v2/common"
	nknConfig "github.com/nknorg/nkn/v2/config"
	"github.com/nknorg/nkn/v2/program"
	"github.com/nknorg/nkn/v2/transaction"
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
	GetNonceContext(ctx context.Context, txPool bool) (int64, error)
	GetNonceByAddress(address string, txPool bool) (int64, error)
	GetNonceByAddressContext(ctx context.Context, address string, txPool bool) (int64, error)
	Balance() (*Amount, error)
	BalanceContext(ctx context.Context) (*Amount, error)
	BalanceByAddress(address string) (*Amount, error)
	BalanceByAddressContext(ctx context.Context, address string) (*Amount, error)
	GetHeight() (int32, error)
	GetHeightContext(ctx context.Context) (int32, error)
	GetSubscribers(topic string, offset, limit int, meta, txPool bool, subscriberHashPrefix []byte) (*Subscribers, error)
	GetSubscribersContext(ctx context.Context, topic string, offset, limit int, meta, txPool bool, subscriberHashPrefix []byte) (*Subscribers, error)
	GetSubscription(topic string, subscriber string) (*Subscription, error)
	GetSubscriptionContext(ctx context.Context, topic string, subscriber string) (*Subscription, error)
	GetSubscribersCount(topic string, subscriberHashPrefix []byte) (int, error)
	GetSubscribersCountContext(ctx context.Context, topic string, subscriberHashPrefix []byte) (int, error)
	GetRegistrant(name string) (*Registrant, error)
	GetRegistrantContext(ctx context.Context, name string) (*Registrant, error)
	SendRawTransaction(txn *transaction.Transaction) (string, error)
	SendRawTransactionContext(ctx context.Context, txn *transaction.Transaction) (string, error)
}

// SignerRPCClient is a RPCClient that can also sign transactions and made RPC
// requests that requires signatures.
type signerRPCClient interface {
	signer
	rpcClient
	Transfer(address, amount string, config *TransactionConfig) (string, error)
	TransferContext(ctx context.Context, address, amount string, config *TransactionConfig) (string, error)
	RegisterName(name string, config *TransactionConfig) (string, error)
	RegisterNameContext(ctx context.Context, name string, config *TransactionConfig) (string, error)
	TransferName(name string, recipientPubKey []byte, config *TransactionConfig) (string, error)
	TransferNameContext(ctx context.Context, name string, recipientPubKey []byte, config *TransactionConfig) (string, error)
	DeleteName(name string, config *TransactionConfig) (string, error)
	DeleteNameContext(ctx context.Context, name string, config *TransactionConfig) (string, error)
	Subscribe(identifier, topic string, duration int, meta string, config *TransactionConfig) (string, error)
	SubscribeContext(ctx context.Context, identifier, topic string, duration int, meta string, config *TransactionConfig) (string, error)
	Unsubscribe(identifier, topic string, config *TransactionConfig) (string, error)
	UnsubscribeContext(ctx context.Context, identifier, topic string, config *TransactionConfig) (string, error)
}

// RPCConfigInterface is the config interface for making rpc call. ClientConfig,
// WalletConfig and RPCConfig all implement this interface and thus can be used
// directly. RPC prefix is added to all public methods to avoid gomobile compile
// error.
type RPCConfigInterface interface {
	RPCGetSeedRPCServerAddr() *StringArray
	RPCGetRPCTimeout() int32
	RPCGetConcurrency() int32
}

// Node struct contains the information of the node that a client connects to.
type Node struct {
	Addr    string `json:"addr"`
	RPCAddr string `json:"rpcAddr"`
	PubKey  string `json:"pubkey"`
	ID      string `json:"id"`
}

// NodeState struct contains the state of a NKN full node.
type NodeState struct {
	Addr               string `json:"addr"`
	CurrTimeStamp      int64  `json:"currTimeStamp"`
	Height             int32  `json:"height"` // Changed to signed int for gomobile compatibility
	ID                 string `json:"id"`
	JSONRPCPort        int32  `json:"jsonRpcPort"`
	ProposalSubmitted  int32  `json:"proposalSubmitted"`
	ProtocolVersion    int32  `json:"protocolVersion"`
	PublicKey          string `json:"publicKey"`
	RelayMessageCount  int64  `json:"relayMessageCount"`
	SyncState          string `json:"syncState"`
	TLSJSONRpcDomain   string `json:"tlsJsonRpcDomain"`
	TLSJSONRpcPort     int32  `json:"tlsJsonRpcPort"`
	TLSWebsocketDomain string `json:"tlsWebsocketDomain"`
	TLSWebsocketPort   int32  `json:"tlsWebsocketPort"`
	Uptime             int64  `json:"uptime"`
	Version            string `json:"version"`
	WebsocketPort      int32  `json:"websocketPort"`
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

func httpPost(ctx context.Context, addr string, reqBody []byte, timeout time.Duration) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", addr, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}
	client := &http.Client{
		Timeout: timeout,
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return ioutil.ReadAll(resp.Body)
}

// RPCCall makes a RPC call and put results to result passed in.
func RPCCall(parentCtx context.Context, method string, params map[string]interface{}, result interface{}, config RPCConfigInterface) error {
	if config == nil {
		config = GetDefaultRPCConfig()
	}

	req, err := json.Marshal(map[string]interface{}{
		"id":     "nkn-sdk-go",
		"method": method,
		"params": params,
	})
	if err != nil {
		return &errorWithCode{err: err, code: errCodeEncodeError}
	}

	var wg sync.WaitGroup
	var respLock sync.Mutex
	rpcAddrChan := make(chan string)
	respErrChan := make(chan *errResp, 1)
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	n := int(config.RPCGetConcurrency())
	if n == 0 {
		n = config.RPCGetSeedRPCServerAddr().Len()
	}
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for rpcAddr := range rpcAddrChan {
				body, err := httpPost(ctx, rpcAddr, req, time.Duration(config.RPCGetRPCTimeout())*time.Millisecond)
				if err != nil {
					select {
					case <-ctx.Done():
						return
					default:
						log.Println(err)
					}
					continue
				}

				respJSON := make(map[string]*json.RawMessage)
				err = json.Unmarshal(body, &respJSON)
				if err != nil {
					log.Println(err)
					continue
				}
				if respJSON["error"] != nil {
					er := &errResp{}
					err := json.Unmarshal(*respJSON["error"], er)
					if err != nil {
						log.Println(err)
						continue
					}
					select {
					case respErrChan <- er:
						cancel()
					case <-ctx.Done():
						return
					}
					return
				}

				respLock.Lock()
				select {
				case <-ctx.Done():
					respLock.Unlock()
					return
				default:
				}
				err = json.Unmarshal(*respJSON["result"], result)
				if err != nil {
					log.Println(err)
					respLock.Unlock()
					continue
				}

				select {
				case respErrChan <- nil:
					cancel()
					respLock.Unlock()
				case <-ctx.Done():
					respLock.Unlock()
					return
				}
			}
		}()
	}

	for _, rpcAddr := range config.RPCGetSeedRPCServerAddr().Elems() {
		select {
		case rpcAddrChan <- rpcAddr:
		case <-ctx.Done():
			break
		}
	}

	close(rpcAddrChan)

	wg.Wait()

	close(respErrChan)

	err = parentCtx.Err()
	if err != nil {
		return &errorWithCode{err: err, code: errCodeCanceled}
	}

	er, ok := <-respErrChan
	if !ok {
		return &errorWithCode{err: errors.New("all rpc request failed"), code: errCodeNetworkError}
	}

	if er != nil {
		msg := er.Message
		if len(er.Data) > 0 {
			msg += ": " + er.Data
		}
		return &errorWithCode{err: errors.New(msg), code: er.Code}
	}

	return nil
}

// GetWsAddr wraps GetWsAddrContext with background context.
func GetWsAddr(clientAddr string, config RPCConfigInterface) (*Node, error) {
	return GetWsAddrContext(context.Background(), clientAddr, config)
}

// GetWsAddrContext RPC gets the node that a client address should connect to
// using ws.
func GetWsAddrContext(ctx context.Context, clientAddr string, config RPCConfigInterface) (*Node, error) {
	node := &Node{}
	err := RPCCall(ctx, "getwsaddr", map[string]interface{}{"address": clientAddr}, node, config)
	if err != nil {
		return nil, err
	}
	return node, nil
}

// GetWssAddr wraps GetWssAddrContext with background context.
func GetWssAddr(clientAddr string, config RPCConfigInterface) (*Node, error) {
	return GetWssAddrContext(context.Background(), clientAddr, config)
}

// GetWssAddrContext RPC gets the node that a client address should connect to
// using wss.
func GetWssAddrContext(ctx context.Context, clientAddr string, config RPCConfigInterface) (*Node, error) {
	node := &Node{}
	err := RPCCall(ctx, "getwssaddr", map[string]interface{}{"address": clientAddr}, node, config)
	if err != nil {
		return nil, err
	}
	return node, nil
}

// GetNonce wraps GetNonceContext with background context.
func GetNonce(address string, txPool bool, config RPCConfigInterface) (int64, error) {
	return GetNonceContext(context.Background(), address, txPool, config)
}

// GetNonceContext RPC gets the next nonce to use of an address. If txPool is
// false, result only counts transactions in ledger; if txPool is true,
// transactions in txPool are also counted.
//
// Nonce is changed to signed int for gomobile compatibility.
func GetNonceContext(ctx context.Context, address string, txPool bool, config RPCConfigInterface) (int64, error) {
	n := &nonce{}
	err := RPCCall(ctx, "getnoncebyaddr", map[string]interface{}{"address": address}, n, config)
	if err != nil {
		return 0, err
	}
	if txPool && n.NonceInTxPool > n.Nonce {
		return int64(n.NonceInTxPool), nil
	}
	return int64(n.Nonce), nil
}

// GetBalance wraps GetBalanceContext with background context.
func GetBalance(address string, config RPCConfigInterface) (*Amount, error) {
	return GetBalanceContext(context.Background(), address, config)
}

// GetBalanceContext RPC returns the balance of a wallet address.
func GetBalanceContext(ctx context.Context, address string, config RPCConfigInterface) (*Amount, error) {
	balance := &balance{}
	err := RPCCall(ctx, "getbalancebyaddr", map[string]interface{}{"address": address}, balance, config)
	if err != nil {
		return nil, err
	}
	return NewAmount(balance.Amount)
}

// GetHeight wraps GetHeightContext with background context.
func GetHeight(config RPCConfigInterface) (int32, error) {
	return GetHeightContext(context.Background(), config)
}

// GetHeightContext RPC returns the latest block height.
func GetHeightContext(ctx context.Context, config RPCConfigInterface) (int32, error) {
	var height uint32
	err := RPCCall(ctx, "getlatestblockheight", map[string]interface{}{}, &height, config)
	if err != nil {
		return 0, err
	}
	return int32(height), nil
}

// GetSubscribers wraps GetSubscribersContext with background context.
func GetSubscribers(topic string, offset, limit int, meta, txPool bool, subscriberHashPrefix []byte, config RPCConfigInterface) (*Subscribers, error) {
	return GetSubscribersContext(context.Background(), topic, offset, limit, meta, txPool, subscriberHashPrefix, config)
}

// GetSubscribersContext gets the subscribers of a topic with a offset and max
// number of results (limit). If meta is true, results contain each subscriber's
// metadata. If txPool is true, results contain subscribers in txPool. Enabling
// this will get subscribers sooner after they send subscribe transactions, but
// might affect the correctness of subscribers because transactions in txpool is
// not guaranteed to be packed into a block. If subscriberHashPrefix is not
// empty, only subscriber whose sha256(pubkey+identifier) contains this prefix
// will be returned. Each prefix byte will reduce result count to about 1/256,
// and also reduce response time to about 1/256 if there are a lot of
// subscribers. This is a good way to sample subscribers randomly with low cost.
//
// Offset and limit are changed to signed int for gomobile compatibility
func GetSubscribersContext(ctx context.Context, topic string, offset, limit int, meta, txPool bool, subscriberHashPrefix []byte, config RPCConfigInterface) (*Subscribers, error) {
	var result map[string]interface{}
	err := RPCCall(ctx, "getsubscribers", map[string]interface{}{
		"topic":                topic,
		"offset":               offset,
		"limit":                limit,
		"meta":                 meta,
		"txPool":               txPool,
		"subscriberHashPrefix": hex.EncodeToString(subscriberHashPrefix),
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

// GetSubscription wraps GetSubscriptionContext with background context.
func GetSubscription(topic string, subscriber string, config RPCConfigInterface) (*Subscription, error) {
	return GetSubscriptionContext(context.Background(), topic, subscriber, config)
}

// GetSubscriptionContext RPC gets the subscription details of a subscriber in a topic.
func GetSubscriptionContext(ctx context.Context, topic string, subscriber string, config RPCConfigInterface) (*Subscription, error) {
	subscription := &Subscription{}
	err := RPCCall(ctx, "getsubscription", map[string]interface{}{
		"topic":      topic,
		"subscriber": subscriber,
	}, subscription, config)
	if err != nil {
		return nil, err
	}
	return subscription, nil
}

// GetSubscribersCount wraps GetSubscribersCountContext with background context.
func GetSubscribersCount(topic string, subscriberHashPrefix []byte, config RPCConfigInterface) (int, error) {
	return GetSubscribersCountContext(context.Background(), topic, subscriberHashPrefix, config)
}

// GetSubscribersCountContext RPC returns the number of subscribers of a topic
// (not including txPool). If subscriberHashPrefix is not empty, only subscriber
// whose sha256(pubkey+identifier) contains this prefix will be counted. Each
// prefix byte will reduce result count to about 1/256, and also reduce response
// time to about 1/256 if there are a lot of subscribers. This is a good way to
// sample subscribers randomly with low cost.
//
// Count is changed to signed int for gomobile compatibility
func GetSubscribersCountContext(ctx context.Context, topic string, subscriberHashPrefix []byte, config RPCConfigInterface) (int, error) {
	var count int
	err := RPCCall(ctx, "getsubscriberscount", map[string]interface{}{
		"topic":                topic,
		"subscriberHashPrefix": hex.EncodeToString(subscriberHashPrefix),
	}, &count, config)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// GetRegistrant wraps GetRegistrantContext with background context.
func GetRegistrant(name string, config RPCConfigInterface) (*Registrant, error) {
	return GetRegistrantContext(context.Background(), name, config)
}

// GetRegistrantContext RPC gets the registrant of a name.
func GetRegistrantContext(ctx context.Context, name string, config RPCConfigInterface) (*Registrant, error) {
	registrant := &Registrant{}
	err := RPCCall(ctx, "getregistrant", map[string]interface{}{"name": name}, registrant, config)
	if err != nil {
		return nil, err
	}
	return registrant, nil
}

// SendRawTransaction wraps SendRawTransactionContext with background context.
func SendRawTransaction(txn *transaction.Transaction, config RPCConfigInterface) (string, error) {
	return SendRawTransactionContext(context.Background(), txn, config)
}

// SendRawTransactionContext RPC sends a signed transaction to chain and returns
// txn hash hex string.
func SendRawTransactionContext(ctx context.Context, txn *transaction.Transaction, config RPCConfigInterface) (string, error) {
	b, err := txn.Marshal()
	if err != nil {
		return "", err
	}
	var txnHash string
	err = RPCCall(ctx, "sendrawtransaction", map[string]interface{}{"tx": hex.EncodeToString(b)}, &txnHash, config)
	if err != nil {
		return "", err
	}
	return txnHash, nil
}

// Transfer wraps TransferContext with background context.
func Transfer(s signerRPCClient, address, amount string, config *TransactionConfig) (string, error) {
	return TransferContext(context.Background(), s, address, amount, config)
}

// TransferContext sends asset to a wallet address with a transaction fee.
// Amount is the string representation of the amount in unit of NKN to avoid
// precision loss. For example, "0.1" will be parsed as 0.1 NKN. The
// signerRPCClient can be a client, multiclient or wallet.
func TransferContext(ctx context.Context, s signerRPCClient, address, amount string, config *TransactionConfig) (string, error) {
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

	return s.SendRawTransactionContext(ctx, tx)
}

// RegisterName wraps RegisterNameContext with background context.
func RegisterName(s signerRPCClient, name string, config *TransactionConfig) (string, error) {
	return RegisterNameContext(context.Background(), s, name, config)
}

// RegisterNameContext registers a name for this signer's public key at the cost
// of 10 NKN with a given transaction fee. The name will be valid for 1,576,800
// blocks (around 1 year). Register name currently owned by this pubkey will
// extend the duration of the name to current block height + 1,576,800.
// Registration will fail if the name is currently owned by another account. The
// signerRPCClient can be a client, multiclient or wallet.
func RegisterNameContext(ctx context.Context, s signerRPCClient, name string, config *TransactionConfig) (string, error) {
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

	return s.SendRawTransactionContext(ctx, tx)
}

// TransferName wraps TransferNameContext with background context.
func TransferName(s signerRPCClient, name string, recipientPubKey []byte, config *TransactionConfig) (string, error) {
	return TransferNameContext(context.Background(), s, name, recipientPubKey, config)
}

// TransferNameContext transfers a name owned by this signer's pubkey to another
// public key with a transaction fee. The expiration height of the name will not
// be changed. The signerRPCClient can be a client, multiclient or wallet.
func TransferNameContext(ctx context.Context, s signerRPCClient, name string, recipientPubKey []byte, config *TransactionConfig) (string, error) {
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

	return s.SendRawTransactionContext(ctx, tx)
}

// DeleteName wraps DeleteNameContext with background context.
func DeleteName(s signerRPCClient, name string, config *TransactionConfig) (string, error) {
	return DeleteNameContext(context.Background(), s, name, config)
}

// DeleteNameContext deletes a name owned by this signer's pubkey with a given
// transaction fee. The signerRPCClient can be a client, multiclient or wallet.
func DeleteNameContext(ctx context.Context, s signerRPCClient, name string, config *TransactionConfig) (string, error) {
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

	return s.SendRawTransactionContext(ctx, tx)
}

// Subscribe wraps SubscribeContext with background context.
func Subscribe(s signerRPCClient, identifier, topic string, duration int, meta string, config *TransactionConfig) (string, error) {
	return SubscribeContext(context.Background(), s, identifier, topic, duration, meta, config)
}

// SubscribeContext to a topic with an identifier for a number of blocks. Client
// using the same key pair and identifier will be able to receive messages from
// this topic. If this (identifier, public key) pair is already subscribed to
// this topic, the subscription expiration will be extended to current block
// height + duration. The signerRPCClient can be a client, multiclient or
// wallet.
//
// Duration is changed to signed int for gomobile compatibility.
func SubscribeContext(ctx context.Context, s signerRPCClient, identifier, topic string, duration int, meta string, config *TransactionConfig) (string, error) {
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

	return s.SendRawTransactionContext(ctx, tx)
}

// Unsubscribe wraps UnsubscribeContext with background context.
func Unsubscribe(s signerRPCClient, identifier, topic string, config *TransactionConfig) (string, error) {
	return UnsubscribeContext(context.Background(), s, identifier, topic, config)
}

// UnsubscribeContext from a topic for an identifier. Client using the same key
// pair and identifier will no longer receive messages from this topic. The
// signerRPCClient can be a client, multiclient or wallet.
func UnsubscribeContext(ctx context.Context, s signerRPCClient, identifier, topic string, config *TransactionConfig) (string, error) {
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

	return s.SendRawTransactionContext(ctx, tx)
}

// GetNodeState wraps GetNodeStateContext with background context.
func GetNodeState(config RPCConfigInterface) (*NodeState, error) {
	return GetNodeStateContext(context.Background(), config)
}

// GetNodeStateContext returns the state of the RPC node.
func GetNodeStateContext(ctx context.Context, config RPCConfigInterface) (*NodeState, error) {
	nodeState := &NodeState{}
	err := RPCCall(ctx, "getnodestate", map[string]interface{}{}, nodeState, config)
	if err != nil {
		return nil, err
	}
	return nodeState, nil
}
