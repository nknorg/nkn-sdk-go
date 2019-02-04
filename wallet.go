package nkn_sdk_go

import (
	"errors"
	"math"
	"sort"

	"github.com/nknorg/nkn/common"
	"github.com/nknorg/nkn/core/contract"
	"github.com/nknorg/nkn/core/signature"
	"github.com/nknorg/nkn/core/transaction"
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

type utxoUnspentInfo struct {
	Txid  string
	Index uint32
	Value float64
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
	ct, err := contract.CreateSignatureContract(w.account.PublicKey)
	if err != nil {
		return err
	}
	ctx := &contract.ContractContext{
		Data:            tx,
		ProgramHashes:   []common.Uint160{ct.ProgramHash},
		Codes:           make([][]byte, 1),
		Parameters:      make([][][]byte, 1),
		MultiPubkeyPara: make([][]contract.PubkeyParameter, 1),
	}

	sig, err := signature.SignBySigner(tx, w.account)
	if err != nil {
		return err
	}
	err = ctx.AddContract(ct, w.account.PublicKey, sig)
	if err != nil {
		return err
	}

	tx.SetPrograms(ctx.GetPrograms())
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

func (w *WalletSDK) getUTXO(address string) ([]*transaction.UTXOUnspent, error) {
	var utxoInfoList []utxoUnspentInfo
	err, _ := call(w.config.SeedRPCServerAddr, "getunspendoutput", map[string]interface{}{"address": address, "assetid": AssetId.ToHexString()}, &utxoInfoList)
	if err != nil {
		return nil, err
	}

	utxoList := make([]*transaction.UTXOUnspent, len(utxoInfoList))
	for i, v := range utxoInfoList {
		txidBytes, err := common.HexStringToBytesReverse(v.Txid)
		if err != nil {
			return nil, err
		}
		txid, err := common.Uint256ParseFromBytes(txidBytes)
		if err != nil {
			return nil, err
		}
		val := common.Fixed64(v.Value * math.Pow(10, 8))
		utxoList[i] = &transaction.UTXOUnspent{
			Txid:  txid,
			Index: v.Index,
			Value: val,
		}
	}

	sort.SliceStable(utxoList, func(i, j int) bool {
		return utxoList[i].Value < utxoList[j].Value
	})

	return utxoList, nil
}

func (w *WalletSDK) Balance() (common.Fixed64, error) {
	address, err := w.account.ProgramHash.ToAddress()
	if err != nil {
		return common.Fixed64(-1), err
	}
	utxoList, err := w.getUTXO(address)
	if err != nil {
		return common.Fixed64(-1), err
	}

	result := common.Fixed64(0)

	for _, input := range utxoList {
		result += input.Value
	}

	return result, nil
}

func (w *WalletSDK) Transfer(address string, value string) (string, error) {
	outputValue, err := common.StringToFixed64(value)
	if err != nil {
		return "", err
	}
	programHash, err := common.ToScriptHash(address)
	if err != nil {
		return "", err
	}
	output := []*transaction.TxnOutput{{
		AssetID:     AssetId,
		Value:       outputValue,
		ProgramHash: programHash,
	}}

	utxoList, err := w.getUTXO(address)
	if err != nil {
		return "", err
	}
	var expected common.Fixed64
	var input []*transaction.TxnInput
	for _, item := range utxoList {
		tmpInput := &transaction.TxnInput{
			ReferTxID:          item.Txid,
			ReferTxOutputIndex: uint16(item.Index),
		}
		input = append(input, tmpInput)
		if item.Value > expected {
			changes := &transaction.TxnOutput{
				AssetID:     AssetId,
				Value:       item.Value - expected,
				ProgramHash: w.account.ProgramHash,
			}
			output = append(output, changes)
			expected = 0
			break
		} else if item.Value == expected {
			expected = 0
			break
		} else if item.Value < expected {
			expected = expected - item.Value
		}
	}

	if expected > 0 {
		return "", errors.New("token is not enough")
	}

	tx, err := transaction.NewTransferAssetTransaction(input, output)
	if err != nil {
		return "", err
	}
	id, err, _ := w.sendRawTransaction(tx)
	return id, err
}

func (w *WalletSDK) RegisterName(name string) (string, error) {
	registrant, err := w.account.PublicKey.EncodePoint(true)
	if err != nil {
		return "", err
	}
	tx, err := transaction.NewRegisterNameTransaction(registrant, name)
	if err != nil {
		return "", err
	}
	id, err, _ := w.sendRawTransaction(tx)
	return id, err
}

func (w *WalletSDK) DeleteName(name string) (string, error) {
	registrant, err := w.account.PublicKey.EncodePoint(true)
	if err != nil {
		return "", err
	}
	tx, err := transaction.NewDeleteNameTransaction(registrant, name)
	if err != nil {
		return "", err
	}
	id, err, _ := w.sendRawTransaction(tx)
	return id, err
}

func (w *WalletSDK) subscribe(identifier string, topic string, bucket uint32, duration uint32, meta string) (string, error, int32) {
	subscriber, err := w.account.PublicKey.EncodePoint(true)
	if err != nil {
		return "", err, -1
	}
	tx, err := transaction.NewSubscribeTransaction(subscriber, identifier, topic, bucket, duration, meta)
	if err != nil {
		return "", err, -1
	}
	return w.sendRawTransaction(tx)
}

func (w *WalletSDK) Subscribe(identifier string, topic string, bucket uint32, duration uint32, meta string) (string, error) {
	id, err, _ := w.subscribe(identifier, topic, bucket, duration, meta)
	return id, err
}

func (w *WalletSDK) SubscribeToFirstAvailableBucket(identifier string, topic string, duration uint32, meta string) (string, error) {
	for {
		bucket, err := w.GetFirstAvailableTopicBucket(topic)
		if err != nil {
			return "", err
		}
		if bucket == -1 {
			return "", errors.New("no more free buckets")
		}
		id, err, code := w.subscribe(identifier, topic, uint32(bucket), duration, meta)
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