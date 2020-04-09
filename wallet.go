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

// Wallet manages assets, query state from blockchain, and send transactions to
// blockchain.
type Wallet struct {
	config             *WalletConfig
	account            *Account
	address            string
	seedEncrypted      []byte
	iv                 []byte
	masterKeyEncrypted []byte
	passwordHash       []byte
	contractData       []byte
}

// NewWallet creates a wallet from an account and an optional config. For any
// zero value field in config, the default wallet config value will be used. If
// config is nil, the default wallet config will be used. However, it is
// strongly recommended to use non-empty password in config to protect the
// wallet, otherwise anyone can recover the wallet and control all assets in the
// wallet from the generated wallet JSON.
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

	wallet := &Wallet{
		config:             config,
		account:            account,
		address:            account.WalletAddress(),
		seedEncrypted:      seedEncrypted,
		iv:                 iv,
		masterKeyEncrypted: masterKeyEncrypted,
		passwordHash:       passwordHash[:],
		contractData:       contract.ToArray(),
	}

	return wallet, nil
}

// WalletFromJSON recovers a wallet from wallet JSON and wallet config. The
// password in config must match the password used to create the wallet.
func WalletFromJSON(walletJSON string, config *WalletConfig) (*Wallet, error) {
	walletData := &vault.WalletData{}
	err := json.Unmarshal([]byte(walletJSON), walletData)
	if err != nil {
		return nil, err
	}

	if walletData.Version < vault.MinCompatibleWalletVersion || walletData.Version > vault.MaxCompatibleWalletVersion {
		return nil, fmt.Errorf("invalid wallet version %v, should be between %v and %v", walletData.Version, vault.MinCompatibleWalletVersion, vault.MaxCompatibleWalletVersion)
	}

	if config == nil {
		config = &WalletConfig{}
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

// ToJSON serialize the wallet to JSON string encrypted by password used to
// create the wallet. The same password must be used to recover the wallet from
// JSON string.
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

// Seed returns the secret seed of the wallet. Secret seed can be used to create
// client/wallet with the same key pair and should be kept secret and safe.
func (w *Wallet) Seed() []byte {
	return w.account.Seed()
}

// PubKey returns the public key of the wallet.
func (w *Wallet) PubKey() []byte {
	return w.account.PubKey()
}

// Address returns the NKN wallet address of the wallet.
func (w *Wallet) Address() string {
	return w.address
}

// VerifyPassword returns whether a password is the correct password of this
// wallet.
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

// SendRawTransaction sends a signed transaction to blockchain and returns
// the hex string of transaction hash.
func (w *Wallet) SendRawTransaction(txn *transaction.Transaction) (string, error) {
	return SendRawTransaction(txn, w.config)
}

// GetNonce returns the next nonce of this wallet to use. If txPool is false,
// result only counts transactions in ledger; if txPool is true, transactions in
// txPool are also counted.
//
// Nonce is changed to signed int for gomobile compatibility.
func (w *Wallet) GetNonce(txPool bool) (int64, error) {
	return GetNonce(w.address, txPool, w.config)
}

// GetHeight returns the latest block height.
func (w *Wallet) GetHeight() (int32, error) {
	return GetHeight(w.config)
}

// Balance returns the balance of this wallet.
func (w *Wallet) Balance() (*Amount, error) {
	return w.BalanceByAddress(w.address)
}

// BalanceByAddress returns the balance of a wallet address.
func (w *Wallet) BalanceByAddress(address string) (*Amount, error) {
	return GetBalance(address, w.config)
}

// Transfer sends asset to a wallet address with a transaction fee. Both amount
// and fee are the string representation of the amount in unit of NKN to avoid
// precision loss. For example, "0.1" will be parsed as 0.1 NKN.
func (w *Wallet) Transfer(address, amount, fee string) (string, error) {
	amountFixed64, err := common.StringToFixed64(amount)
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

	nonce, err := w.GetNonce(true)
	if err != nil {
		return "", err
	}

	tx, err := transaction.NewTransferAssetTransaction(w.account.ProgramHash, programHash, uint64(nonce), amountFixed64, _fee)
	if err != nil {
		return "", err
	}

	if err := w.signTransaction(tx); err != nil {
		return "", err
	}

	return w.SendRawTransaction(tx)
}

// NewNanoPay is a shortcut for NewNanoPay using this wallet as sender.
//
// Duration is changed to signed int for gomobile compatibility.
func (w *Wallet) NewNanoPay(recipientAddress, fee string, duration int) (*NanoPay, error) {
	return NewNanoPay(w, recipientAddress, fee, duration)
}

// NewNanoPayClaimer is a shortcut for NewNanoPayClaimer using this wallet's RPC
// server address.
func (w *Wallet) NewNanoPayClaimer(recipientAddress string, claimIntervalMs int32, onError *OnError) (*NanoPayClaimer, error) {
	if len(recipientAddress) == 0 {
		recipientAddress = w.Address()
	}
	return NewNanoPayClaimer(recipientAddress, claimIntervalMs, onError, w.config)
}

// RegisterName registers a name for this wallet's public key at the cost of 10
// NKN with a given transaction fee. The name will be valid for 1,576,800 blocks
// (around 1 year). Register name currently owned by this wallet will extend the
// duration of the name to current block height + 1,576,800. Registration will
// fail if the name is currently owned by another account.
func (w *Wallet) RegisterName(name, fee string) (string, error) {
	_fee, err := common.StringToFixed64(fee)
	if err != nil {
		return "", err
	}

	nonce, err := w.GetNonce(true)
	if err != nil {
		return "", err
	}

	tx, err := transaction.NewRegisterNameTransaction(w.PubKey(), name, uint64(nonce), config.MinNameRegistrationFee, _fee)
	if err != nil {
		return "", err
	}

	if err := w.signTransaction(tx); err != nil {
		return "", err
	}

	return w.SendRawTransaction(tx)
}

// TransferName transfers a name owned by this wallet to another public key with
// a transaction fee. The expiration height of the name will not be changed.
func (w *Wallet) TransferName(name string, recipientPubKey []byte, fee string) (string, error) {
	_fee, err := common.StringToFixed64(fee)
	if err != nil {
		return "", err
	}

	nonce, err := w.GetNonce(true)
	if err != nil {
		return "", err
	}

	tx, err := transaction.NewTransferNameTransaction(w.PubKey(), recipientPubKey, name, uint64(nonce), _fee)
	if err != nil {
		return "", err
	}

	if err := w.signTransaction(tx); err != nil {
		return "", err
	}

	return w.SendRawTransaction(tx)
}

// DeleteName deletes a name owned by this wallet with a given transaction fee.
func (w *Wallet) DeleteName(name, fee string) (string, error) {
	_fee, err := common.StringToFixed64(fee)
	if err != nil {
		return "", err
	}

	nonce, err := w.GetNonce(true)
	if err != nil {
		return "", err
	}

	tx, err := transaction.NewDeleteNameTransaction(w.PubKey(), name, uint64(nonce), _fee)
	if err != nil {
		return "", err
	}

	if err := w.signTransaction(tx); err != nil {
		return "", err
	}

	return w.SendRawTransaction(tx)
}

// Subscribe to a topic with an identifier for a number of blocks. Client using
// the same key pair and identifier will be able to receive messages from this
// topic. If this (identifier, public key) pair is already subscribed to this
// topic, the subscription expiration will be extended to current block height +
// duration.
//
// Duration is changed to signed int for gomobile compatibility.
func (w *Wallet) Subscribe(identifier, topic string, duration int, meta, fee string) (string, error) {
	_fee, err := common.StringToFixed64(fee)
	if err != nil {
		return "", err
	}
	nonce, err := w.GetNonce(true)
	if err != nil {
		return "", err
	}
	tx, err := transaction.NewSubscribeTransaction(
		w.PubKey(),
		identifier,
		topic,
		uint32(duration),
		meta,
		uint64(nonce),
		_fee,
	)
	if err != nil {
		return "", err
	}

	if err := w.signTransaction(tx); err != nil {
		return "", err
	}

	return w.SendRawTransaction(tx)
}

// Unsubscribe from a topic for an identifier. Client using the same key pair
// and identifier will no longer receive messages from this topic.
func (w *Wallet) Unsubscribe(identifier string, topic string, fee string) (string, error) {
	_fee, err := common.StringToFixed64(fee)
	if err != nil {
		return "", err
	}
	nonce, err := w.GetNonce(true)
	if err != nil {
		return "", err
	}
	tx, err := transaction.NewUnsubscribeTransaction(
		w.PubKey(),
		identifier,
		topic,
		uint64(nonce),
		_fee,
	)
	if err != nil {
		return "", err
	}

	if err := w.signTransaction(tx); err != nil {
		return "", err
	}

	return w.SendRawTransaction(tx)
}
