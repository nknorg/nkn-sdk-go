package nkn

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nknorg/nkn/common"
	"github.com/nknorg/nkn/crypto"
	"github.com/nknorg/nkn/pb"
	"github.com/nknorg/nkn/program"
	"github.com/nknorg/nkn/signature"
	"github.com/nknorg/nkn/transaction"
	"github.com/nknorg/nkn/util/config"
	"github.com/nknorg/nkn/vault"
)

type errWithCode struct {
	err  error
	code int32
}

type queuedTx struct {
	tx   *transaction.Transaction
	txId chan string
	err  chan *errWithCode
}

type Wallet struct {
	config             *WalletConfig
	account            *Account
	address            string
	seedEncrypted      []byte
	iv                 []byte
	masterKeyEncrypted []byte
	passwordHash       []byte
	contractData       []byte
	txChannel          chan *queuedTx
}

type Subscription struct {
	Meta      string
	ExpiresAt int32 // changed to signed int for gomobile compatibility
}

type balance struct {
	Amount string `json:"amount"`
}

type nonce struct {
	Nonce         uint64 `json:"nonce"`
	NonceInTxPool uint64 `json:"nonceInTxPool"`
}

func NewWallet(account *Account, config *WalletConfig) (*Wallet, error) {
	config, err := MergeWalletConfig(config)
	if err != nil {
		return nil, err
	}

	defer func() {
		config.Password = ""
		config.MasterKey = nil
	}()

	iv := config.IV
	if iv == nil {
		iv, err = RandomBytes(vault.WalletIVLength)
		if err != nil {
			return nil, err
		}
	}

	masterKey := config.MasterKey
	if masterKey == nil {
		masterKey, err = RandomBytes(vault.WalletMasterKeyLength)
		if err != nil {
			return nil, err
		}
	}

	passwordKey := crypto.ToAesKey([]byte(config.Password))
	passwordHash := sha256.Sum256(passwordKey)

	seedEncrypted, err := crypto.AesEncrypt(account.Seed(), masterKey, iv)
	if err != nil {
		return nil, err
	}

	masterKeyEncrypted, err := crypto.AesEncrypt(masterKey, passwordKey, iv)
	if err != nil {
		return nil, err
	}

	contract, err := program.CreateSignatureProgramContext(account.Account.PubKey())
	if err != nil {
		return nil, err
	}

	txChannel := make(chan *queuedTx)
	go func() {
		for {
			queuedTx := <-txChannel
			tx := queuedTx.tx
			var txId string
			code, err := call(config.GetRandomSeedRPCServerAddr(), "sendrawtransaction", map[string]interface{}{"tx": common.BytesToHexString(tx.ToArray())}, &txId)
			if err != nil {
				queuedTx.err <- &errWithCode{err, code}
			} else {
				queuedTx.txId <- txId
			}
		}
	}()

	wallet := &Wallet{
		config:             config,
		account:            account,
		address:            account.WalletAddress(),
		seedEncrypted:      seedEncrypted,
		iv:                 iv,
		masterKeyEncrypted: masterKeyEncrypted,
		passwordHash:       passwordHash[:],
		contractData:       contract.ToArray(),
		txChannel:          txChannel,
	}

	return wallet, nil
}

func WalletFromJSON(walletJSON string, config *WalletConfig) (*Wallet, error) {
	walletData := &vault.WalletData{}
	err := json.Unmarshal([]byte(walletJSON), walletData)
	if err != nil {
		return nil, err
	}

	if walletData.Version < vault.MinCompatibleWalletVersion || walletData.Version > vault.MaxCompatibleWalletVersion {
		return nil, fmt.Errorf("invalid wallet version %v, should be between %v and %v", walletData.Version, vault.MinCompatibleWalletVersion, vault.MaxCompatibleWalletVersion)
	}

	passwordKey := crypto.ToAesKey([]byte(config.Password))
	passwordKeyHash, err := hex.DecodeString(walletData.PasswordHash)
	if err != nil {
		return nil, err
	}
	pkh := sha256.Sum256(passwordKey)
	if !bytes.Equal(pkh[:], passwordKeyHash) {
		return nil, errors.New("password wrong")
	}

	iv, err := hex.DecodeString(walletData.IV)
	if err != nil {
		return nil, err
	}

	masterKeyEncrypted, err := hex.DecodeString(walletData.MasterKey)
	if err != nil {
		return nil, err
	}

	masterKey, err := crypto.AesDecrypt(masterKeyEncrypted, passwordKey, iv)
	if err != nil {
		return nil, err
	}

	seedEncrypted, err := hex.DecodeString(walletData.SeedEncrypted)
	if err != nil {
		return nil, err
	}

	seed, err := crypto.AesDecrypt(seedEncrypted, masterKey, iv)
	if err != nil {
		return nil, err
	}

	account, err := NewAccount(seed)
	if err != nil {
		return nil, err
	}

	configCopy := *config
	configCopy.IV = iv
	configCopy.MasterKey = masterKey

	return NewWallet(account, &configCopy)
}

func (w *Wallet) ToJSON() (string, error) {
	b, err := json.Marshal(vault.WalletData{
		HeaderData: vault.HeaderData{
			PasswordHash: hex.EncodeToString(w.passwordHash),
			IV:           hex.EncodeToString(w.iv),
			MasterKey:    hex.EncodeToString(w.masterKeyEncrypted),
			Version:      vault.WalletVersion,
		},
		AccountData: vault.AccountData{
			Address:       w.address,
			ProgramHash:   w.account.ProgramHash.ToHexString(),
			SeedEncrypted: hex.EncodeToString(w.seedEncrypted),
			ContractData:  hex.EncodeToString(w.contractData),
		},
	})
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (w *Wallet) Seed() []byte {
	return w.account.Seed()
}

func (w *Wallet) PubKey() []byte {
	return w.account.PubKey()
}

func (w *Wallet) Address() string {
	return w.address
}

func (w *Wallet) VerifyPassword(password string) bool {
	passwordKey := crypto.ToAesKey([]byte(password))
	passwordHash := sha256.Sum256(passwordKey)
	return bytes.Equal(passwordHash[:], w.passwordHash)
}

func (w *Wallet) signTransaction(tx *transaction.Transaction) error {
	ct, err := program.CreateSignatureProgramContext(w.account.PublicKey)
	if err != nil {
		return err
	}

	sig, err := signature.SignBySigner(tx, w.account.Account)
	if err != nil {
		return err
	}

	tx.SetPrograms([]*pb.Program{ct.NewProgram(sig)})
	return nil
}

func (w *Wallet) sendRawTransaction(tx *transaction.Transaction) (string, error, int32) {
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

func (w *Wallet) SendRawTransaction(tx *transaction.Transaction) (string, error) {
	txId, err, _ := w.sendRawTransaction(tx)
	return txId, err
}

// nonce is changed to signed int for gomobile compatibility
func (w *Wallet) GetNonce() (int64, error) {
	nonce, err := w.getNonce()
	return int64(nonce), err
}

func (w *Wallet) getNonce() (uint64, error) {
	var nonce nonce
	_, err := call(w.config.GetRandomSeedRPCServerAddr(), "getnoncebyaddr", map[string]interface{}{"address": w.address}, &nonce)
	if err != nil {
		return 0, err
	}

	if nonce.NonceInTxPool > nonce.Nonce {
		return nonce.NonceInTxPool, nil
	}
	return nonce.Nonce, nil
}

func (w *Wallet) getHeight() (uint32, error) {
	var height uint32
	_, err := call(w.config.GetRandomSeedRPCServerAddr(), "getlatestblockheight", map[string]interface{}{}, &height)
	if err != nil {
		return 0, err
	}

	return height, nil
}

func (w *Wallet) Balance() (*Amount, error) {
	return w.BalanceByAddress(w.address)
}

func (w *Wallet) BalanceByAddress(address string) (*Amount, error) {
	var balance balance
	_, err := call(w.config.GetRandomSeedRPCServerAddr(), "getbalancebyaddr", map[string]interface{}{"address": address}, &balance)
	if err != nil {
		return nil, err
	}

	return NewAmount(balance.Amount)
}

func (w *Wallet) Transfer(address string, value string, fee string) (string, error) {
	outputValue, err := common.StringToFixed64(value)
	if err != nil {
		return "", err
	}

	programHash, err := common.ToScriptHash(address)
	if err != nil {
		return "", err
	}

	_fee, err := common.StringToFixed64(fee)
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

	id, err, _ := w.sendRawTransaction(tx)
	return id, err
}

// duration is changed to signed int for gomobile compatibility
func (w *Wallet) NewNanoPay(address string, fee string, duration int) (*NanoPay, error) {
	_fee, err := NewAmount(fee)
	if err != nil {
		return nil, err
	}
	return NewNanoPay(w, address, _fee, duration)
}

func (w *Wallet) NewNanoPayClaimer(claimIntervalMs int32, onError *OnError, address string) (*NanoPayClaimer, error) {
	return NewNanoPayClaimer(w, address, claimIntervalMs, onError)
}

func (w *Wallet) RegisterName(name string, fee string) (string, error) {
	_fee, err := common.StringToFixed64(fee)
	if err != nil {
		return "", err
	}

	nonce, err := w.getNonce()
	if err != nil {
		return "", err
	}

	tx, err := transaction.NewRegisterNameTransaction(w.PubKey(), name, nonce, config.MinNameRegistrationFee, _fee)
	if err != nil {
		return "", err
	}

	if err := w.signTransaction(tx); err != nil {
		return "", err
	}

	id, err, _ := w.sendRawTransaction(tx)
	return id, err
}

func (w *Wallet) TransferName(name string, recipient []byte, fee string) (string, error) {
	_fee, err := common.StringToFixed64(fee)
	if err != nil {
		return "", err
	}

	nonce, err := w.getNonce()
	if err != nil {
		return "", err
	}

	tx, err := transaction.NewTransferNameTransaction(w.PubKey(), recipient, name, nonce, _fee)
	if err != nil {
		return "", err
	}

	if err := w.signTransaction(tx); err != nil {
		return "", err
	}

	id, err, _ := w.sendRawTransaction(tx)
	return id, err
}

func (w *Wallet) DeleteName(name string, fee string) (string, error) {
	_fee, err := common.StringToFixed64(fee)
	if err != nil {
		return "", err
	}

	nonce, err := w.getNonce()
	if err != nil {
		return "", err
	}

	tx, err := transaction.NewDeleteNameTransaction(w.PubKey(), name, nonce, _fee)
	if err != nil {
		return "", err
	}

	if err := w.signTransaction(tx); err != nil {
		return "", err
	}

	id, err, _ := w.sendRawTransaction(tx)
	return id, err
}

// duration is changed to signed int for gomobile compatibility
func (w *Wallet) Subscribe(identifier string, topic string, duration int, meta string, fee string) (string, error) {
	_fee, err := common.StringToFixed64(fee)
	if err != nil {
		return "", err
	}
	nonce, err := w.getNonce()
	if err != nil {
		return "", err
	}
	tx, err := transaction.NewSubscribeTransaction(
		w.PubKey(),
		identifier,
		topic,
		uint32(duration),
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

	id, err, _ := w.sendRawTransaction(tx)
	return id, err
}

func (w *Wallet) Unsubscribe(identifier string, topic string, fee string) (string, error) {
	_fee, err := common.StringToFixed64(fee)
	if err != nil {
		return "", err
	}
	nonce, err := w.getNonce()
	if err != nil {
		return "", err
	}
	tx, err := transaction.NewUnsubscribeTransaction(
		w.PubKey(),
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

	id, err, _ := w.sendRawTransaction(tx)
	return id, err
}

func (w *Wallet) GetSubscription(topic string, subscriber string) (*Subscription, error) {
	var subscription *Subscription
	_, err := call(w.config.GetRandomSeedRPCServerAddr(), "getsubscription", map[string]interface{}{
		"topic":      topic,
		"subscriber": subscriber,
	}, &subscription)
	if err != nil {
		return nil, err
	}
	return subscription, nil
}

// offset and limit are changed to signed int for gomobile compatibility
func getSubscribers(address string, topic string, offset, limit int, meta, txPool bool) (map[string]string, map[string]string, error) {
	var result map[string]interface{}
	_, err := call(address, "getsubscribers", map[string]interface{}{
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

// offset and limit are changed to signed int for gomobile compatibility
func (w *Wallet) GetSubscribers(topic string, offset, limit int, meta, txPool bool) (*Subscribers, error) {
	subscribers, subscribersInTxPool, err := getSubscribers(w.config.GetRandomSeedRPCServerAddr(), topic, offset, limit, meta, txPool)
	if err != nil {
		return nil, err
	}
	s := &Subscribers{
		Subscribers:         NewStringMap(subscribers),
		SubscribersInTxPool: NewStringMap(subscribersInTxPool),
	}
	return s, nil
}

// count is changed to signed int for gomobile compatibility
func (w *Wallet) GetSubscribersCount(topic string) (int, error) {
	var count int
	_, err := call(w.config.GetRandomSeedRPCServerAddr(), "getsubscriberscount", map[string]interface{}{
		"topic": topic,
	}, &count)
	return count, err
}
