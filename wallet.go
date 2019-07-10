package nkn_sdk_go

import (
	"errors"

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

type WalletSDK struct {
	account *vault.Account
	config  WalletConfig
}

type balance struct {
	amount string `json:"amount"`
}

type nonce struct {
	nonce         uint64 `json:"nonce"`
	nonceInTxPool uint64 `json:"nonceInTxPool"`
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
	return &WalletSDK{account, _config}
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

func (w *WalletSDK) sendRawTransaction(tx *transaction.Transaction) (string, error, int32) {
	err := w.signTransaction(tx)
	if err != nil {
		return "", err, -1
	}

	var txid string
	err, code := call(w.config.SeedRPCServerAddr, "sendrawtransaction", map[string]interface{}{"tx": common.BytesToHexString(tx.ToArray())}, &txid)
	if err != nil {
		return "", err, code
	}
	return txid, nil, 0
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

	if nonce.nonceInTxPool > nonce.nonce {
		return nonce.nonceInTxPool, nil
	}
	return nonce.nonce, nil
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
	var balance balance
	err, _ = call(w.config.SeedRPCServerAddr, "getbalancebyaddr", map[string]interface{}{"address": address}, &balance)
	if err != nil {
		return 0, err
	}

	return common.StringToFixed64(balance.amount)
}

func (w *WalletSDK) Transfer(address string, value string, fee... string) (string, error) {
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

	id, err, _ := w.sendRawTransaction(tx)
	return id, err
}

func (w *WalletSDK) NewNanoPay(address string) (*NanoPay, error) {
	programHash, err := common.ToScriptHash(address)
	if err != nil {
		return nil, err
	}
	return NewNanoPay(w, programHash), nil
}

func (w *WalletSDK) RegisterName(name string, fee... string) (string, error) {
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
	id, err, _ := w.sendRawTransaction(tx)
	return id, err
}

func (w *WalletSDK) DeleteName(name string, fee... string) (string, error) {
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
	id, err, _ := w.sendRawTransaction(tx)
	return id, err
}

func (w *WalletSDK) subscribe(identifier string, topic string, bucket uint32, duration uint32, meta string, fee... string) (string, error, int32) {
	_fee, err := getFee(fee)
	if err != nil {
		return "", err, -1
	}
	nonce, err := w.getNonce()
	if err != nil {
		return "", err, -1
	}
	tx, err := transaction.NewSubscribeTransaction(
		w.account.PublicKey.EncodePoint(),
		identifier,
		topic,
		bucket,
		duration,
		meta,
		nonce,
		_fee,
	)
	if err != nil {
		return "", err, -1
	}
	return w.sendRawTransaction(tx)
}

func (w *WalletSDK) Subscribe(identifier string, topic string, bucket uint32, duration uint32, meta string, fee... string) (string, error) {
	id, err, _ := w.subscribe(identifier, topic, bucket, duration, meta, fee...)
	return id, err
}

func (w *WalletSDK) SubscribeToFirstAvailableBucket(identifier string, topic string, duration uint32, meta string, fee... string) (string, error) {
	for {
		bucket, err := w.GetFirstAvailableTopicBucket(topic)
		if err != nil {
			return "", err
		}
		if bucket == -1 {
			return "", errors.New("no more free buckets")
		}
		id, err, code := w.subscribe(identifier, topic, uint32(bucket), duration, meta, fee...)
		if err != nil && code == 45018 {
			continue
		}
		if err != nil && code == 45020 {
			return "", AlreadySubscribed
		}
		return id, err
	}
}

func (w *WalletSDK) GetAddressByName(name string) (string, error) {
	var address string
	err, _ := call(w.config.SeedRPCServerAddr, "getaddressbyname", map[string]interface{}{"name": name}, &address)
	if err != nil {
		return "", err
	}
	return address, nil
}

func getSubscribers(address string, topic string, bucket uint32) (map[string]string, error) {
	var dests map[string]string
	err, _ := call(address, "getsubscribers", map[string]interface{}{"topic": topic, "bucket": bucket}, &dests)
	if err != nil {
		return nil, err
	}
	return dests, nil
}

func (w *WalletSDK) GetSubscribers(topic string, bucket uint32) (map[string]string, error) {
	return getSubscribers(w.config.SeedRPCServerAddr, topic, bucket)
}

func (w *WalletSDK) GetFirstAvailableTopicBucket(topic string) (int, error) {
	var bucket int
	err, _ := call(w.config.SeedRPCServerAddr, "getfirstavailabletopicbucket", map[string]interface{}{"topic": topic}, &bucket)
	if err != nil {
		return -1, err
	}
	return bucket, nil
}

func (w *WalletSDK) GetTopicBucketsCount(topic string) (uint32, error) {
	var bucket uint32
	err, _ := call(w.config.SeedRPCServerAddr, "gettopicbucketscount", map[string]interface{}{"topic": topic}, &bucket)
	if err != nil {
		return 0, err
	}
	return bucket, nil
}