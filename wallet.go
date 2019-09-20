package nkn_sdk_go

import (
	"errors"
	"time"

	"github.com/nknorg/nkn/common"
	"github.com/nknorg/nkn/pb"
	"github.com/nknorg/nkn/program"
	"github.com/nknorg/nkn/signature"
	"github.com/nknorg/nkn/transaction"
	"github.com/nknorg/nkn/vault"
)

var AlreadySubscribed = errors.New("already subscribed to this topic")

type WalletConfig struct {
	SeedRPCServerAddr string
}

type errWithCode struct {
	err  error
	code int32
}

type queuedTx struct {
	tx   *transaction.Transaction
	txId chan string
	err  chan *errWithCode
}

type WalletSDK struct {
	account   *vault.Account
	config    WalletConfig
	txChannel chan *queuedTx
}

type Subscription struct {
	Meta      string
	ExpiresAt uint32
}

type balance struct {
	Amount string `json:"amount"`
}

type nonce struct {
	Nonce         uint64 `json:"nonce"`
	NonceInTxPool uint64 `json:"nonceInTxPool"`
}

func NewWalletSDK(account *vault.Account, config ...WalletConfig) *WalletSDK {
	var _config WalletConfig
	if len(config) == 0 {
		_config = WalletConfig{seedRPCServerAddr}
	} else {
		_config = config[0]
		if _config.SeedRPCServerAddr == "" {
			_config.SeedRPCServerAddr = seedRPCServerAddr
		}
	}
	txChannel := make(chan *queuedTx)
	go func() {
		for {
			queuedTx := <- txChannel
			tx := queuedTx.tx
			var txId string
			err, code := call(_config.SeedRPCServerAddr, "sendrawtransaction", map[string]interface{}{"tx": common.BytesToHexString(tx.ToArray())}, &txId)
			if err != nil {
				queuedTx.err <- &errWithCode{err, code}
			} else {
				queuedTx.txId <- txId
			}
		}
	}()
	return &WalletSDK{account, _config, txChannel}
}

func (w *WalletSDK) signTransaction(tx *transaction.Transaction) error {
	ct, err := program.CreateSignatureProgramContext(w.account.PublicKey)
	if err != nil {
		return err
	}

	sig, err := signature.SignBySigner(tx, w.account)
	if err != nil {
		return err
	}

	tx.SetPrograms([]*pb.Program{ct.NewProgram(sig)})
	return nil
}

func (w *WalletSDK) SendRawTransaction(tx *transaction.Transaction) (string, error, int32) {
	txIdChan := make(chan string)
	errChan := make(chan *errWithCode)
	w.txChannel <- &queuedTx{tx, txIdChan, errChan}
	select {
	case txId := <-txIdChan:
		return txId, nil, 0
	case err := <-errChan:
		return "", err.err, err.code
	}
}

func (w *WalletSDK) getNonce() (uint64, error) {
	address, err := w.account.ProgramHash.ToAddress()
	if err != nil {
		return 0, err
	}

	var nonce nonce
	err, _ = call(w.config.SeedRPCServerAddr, "getnoncebyaddr", map[string]interface{}{"address": address}, &nonce)
	if err != nil {
		return 0, err
	}

	if nonce.NonceInTxPool > nonce.Nonce {
		return nonce.NonceInTxPool, nil
	}
	return nonce.Nonce, nil
}

func (w *WalletSDK) getHeight() (uint32, error) {
	var height uint32
	err, _ := call(w.config.SeedRPCServerAddr, "getlatestblockheight", map[string]interface{}{}, &height)
	if err != nil {
		return 0, err
	}

	return height, nil
}

func getFee(fee []string) (common.Fixed64, error) {
	if len(fee) == 0 {
		return 0, nil
	}
	return common.StringToFixed64(fee[0])
}

func (w *WalletSDK) Balance() (common.Fixed64, error) {
	address, err := w.account.ProgramHash.ToAddress()
	if err != nil {
		return common.Fixed64(-1), err
	}
	return w.BalanceByAddress(address)
}

func (w *WalletSDK) BalanceByAddress(address string) (common.Fixed64, error) {
	var balance balance
	err, _ := call(w.config.SeedRPCServerAddr, "getbalancebyaddr", map[string]interface{}{"address": address}, &balance)
	if err != nil {
		return 0, err
	}

	return common.StringToFixed64(balance.Amount)
}

func (w *WalletSDK) Transfer(address string, value string, fee ...string) (string, error) {
	outputValue, err := common.StringToFixed64(value)
	if err != nil {
		return "", err
	}
	programHash, err := common.ToScriptHash(address)
	if err != nil {
		return "", err
	}

	_fee, err := getFee(fee)
	if err != nil {
		return "", err
	}

	nonce, err := w.getNonce()
	if err != nil {
		return "", err
	}

	tx, err := transaction.NewTransferAssetTransaction(w.account.ProgramHash, programHash, nonce, outputValue, _fee)
	if err != nil {
		return "", err
	}

	if err := w.signTransaction(tx); err != nil {
		return "", err
	}

	id, err, _ := w.SendRawTransaction(tx)
	return id, err
}

func (w *WalletSDK) NewNanoPay(address string, fee string, duration ...uint32) (*NanoPay, error) {
	_fee, err := common.StringToFixed64(fee)
	if err != nil {
		return nil, err
	}
	return NewNanoPay(w, address, _fee, duration...)
}

func (w *WalletSDK) NewNanoPayClaimer(claimInterval time.Duration, errChan chan error) *NanoPayClaimer {
	return NewNanoPayClaimer(w, claimInterval, errChan)
}

func (w *WalletSDK) RegisterName(name string, fee ...string) (string, error) {
	_fee, err := getFee(fee)
	if err != nil {
		return "", err
	}
	nonce, err := w.getNonce()
	if err != nil {
		return "", err
	}
	tx, err := transaction.NewRegisterNameTransaction(w.account.PublicKey.EncodePoint(), name, nonce, _fee)
	if err != nil {
		return "", err
	}

	if err := w.signTransaction(tx); err != nil {
		return "", err
	}

	id, err, _ := w.SendRawTransaction(tx)
	return id, err
}

func (w *WalletSDK) DeleteName(name string, fee ...string) (string, error) {
	_fee, err := getFee(fee)
	if err != nil {
		return "", err
	}
	nonce, err := w.getNonce()
	if err != nil {
		return "", err
	}
	tx, err := transaction.NewDeleteNameTransaction(w.account.PublicKey.EncodePoint(), name, nonce, _fee)
	if err != nil {
		return "", err
	}

	if err := w.signTransaction(tx); err != nil {
		return "", err
	}

	id, err, _ := w.SendRawTransaction(tx)
	return id, err
}

func (w *WalletSDK) Subscribe(identifier string, topic string, duration uint32, meta string, fee ...string) (string, error) {
	_fee, err := getFee(fee)
	if err != nil {
		return "", err
	}
	nonce, err := w.getNonce()
	if err != nil {
		return "", err
	}
	tx, err := transaction.NewSubscribeTransaction(
		w.account.PublicKey.EncodePoint(),
		identifier,
		topic,
		duration,
		meta,
		nonce,
		_fee,
	)
	if err != nil {
		return "", err
	}

	if err := w.signTransaction(tx); err != nil {
		return "", err
	}

	id, err, _ := w.SendRawTransaction(tx)
	return id, err
}

func (w *WalletSDK) Unsubscribe(identifier string, topic string, fee ...string) (string, error) {
	_fee, err := getFee(fee)
	if err != nil {
		return "", err
	}
	nonce, err := w.getNonce()
	if err != nil {
		return "", err
	}
	tx, err := transaction.NewUnsubscribeTransaction(
		w.account.PublicKey.EncodePoint(),
		identifier,
		topic,
		nonce,
		_fee,
	)
	if err != nil {
		return "", err
	}

	if err := w.signTransaction(tx); err != nil {
		return "", err
	}

	id, err, _ := w.SendRawTransaction(tx)
	return id, err
}

func (w *WalletSDK) GetAddressByName(name string) (string, error) {
	var address string
	err, _ := call(w.config.SeedRPCServerAddr, "getaddressbyname", map[string]interface{}{"name": name}, &address)
	if err != nil {
		return "", err
	}
	return address, nil
}

func (w *WalletSDK) GetSubscription(topic string, subscriber string) (*Subscription, error) {
	var subscription *Subscription
	err, _ := call(w.config.SeedRPCServerAddr, "getsubscription", map[string]interface{}{
		"topic":      topic,
		"subscriber": subscriber,
	}, &subscription)
	if err != nil {
		return nil, err
	}
	return subscription, nil
}

func getSubscribers(address string, topic string, offset, limit uint32, meta, txPool bool) (map[string]string, map[string]string, error) {
	var result map[string]interface{}
	err, _ := call(address, "getsubscribers", map[string]interface{}{
		"topic":  topic,
		"offset": offset,
		"limit":  limit,
		"meta":   meta,
		"txPool": txPool,
	}, &result)
	if err != nil {
		return nil, nil, err
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
	return subscribers, subscribersInTxPool, nil
}

func (w *WalletSDK) GetSubscribers(topic string, offset, limit uint32, meta, txPool bool) (map[string]string, map[string]string, error) {
	return getSubscribers(w.config.SeedRPCServerAddr, topic, offset, limit, meta, txPool)
}

func (w *WalletSDK) GetSubscribersCount(topic string) (uint32, error) {
	var count uint32
	err, _ := call(w.config.SeedRPCServerAddr, "getsubscriberscount", map[string]interface{}{
		"topic":      topic,
	}, &count)
	return count, err
}

