package nkn

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/nknorg/nkn/common"
	"github.com/nknorg/nkn/crypto"
	"github.com/nknorg/nkn/pb"
	"github.com/nknorg/nkn/program"
	"github.com/nknorg/nkn/signature"
	"github.com/nknorg/nkn/transaction"
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
		return nil, ErrInvalidWalletVersion
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
		return nil, ErrWrongPassword
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

// ProgramHash returns the program hash of this wallet's account.
func (w *Wallet) ProgramHash() common.Uint160 {
	return w.account.ProgramHash
}

// SignTransaction signs an unsigned transaction using this wallet's key pair.
func (w *Wallet) SignTransaction(tx *transaction.Transaction) error {
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

// NewNanoPay is a shortcut for NewNanoPay using this wallet as sender.
//
// Duration is changed to signed int for gomobile compatibility.
func (w *Wallet) NewNanoPay(recipientAddress, fee string, duration int) (*NanoPay, error) {
	return NewNanoPay(w, w, recipientAddress, fee, duration)
}

// NewNanoPayClaimer is a shortcut for NewNanoPayClaimer using this wallet as
// RPC client.
func (w *Wallet) NewNanoPayClaimer(recipientAddress string, claimIntervalMs int32, onError *OnError) (*NanoPayClaimer, error) {
	if len(recipientAddress) == 0 {
		recipientAddress = w.Address()
	}
	return NewNanoPayClaimer(recipientAddress, claimIntervalMs, onError, w)
}

// GetNonce is the same as package level GetNonce, but using this wallet's
// SeedRPCServerAddr.
func (w *Wallet) GetNonce(txPool bool) (int64, error) {
	return w.GetNonceByAddress(w.address, txPool)
}

// GetNonceByAddress is the same as package level GetNonce, but using this
// wallet's SeedRPCServerAddr.
func (w *Wallet) GetNonceByAddress(address string, txPool bool) (int64, error) {
	return GetNonce(address, txPool, w.config)
}

// GetHeight is the same as package level GetHeight, but using this wallet's
// SeedRPCServerAddr.
func (w *Wallet) GetHeight() (int32, error) {
	return GetHeight(w.config)
}

// Balance is the same as package level GetBalance, but using this wallet's
// SeedRPCServerAddr.
func (w *Wallet) Balance() (*Amount, error) {
	return w.BalanceByAddress(w.address)
}

// BalanceByAddress is the same as package level GetBalance, but using this
// wallet's SeedRPCServerAddr.
func (w *Wallet) BalanceByAddress(address string) (*Amount, error) {
	return GetBalance(address, w.config)
}

// GetSubscribers is the same as package level GetSubscribers, but using this
// wallet's SeedRPCServerAddr.
func (w *Wallet) GetSubscribers(topic string, offset, limit int, meta, txPool bool) (*Subscribers, error) {
	return GetSubscribers(topic, offset, limit, meta, txPool, w.config)
}

// GetSubscription is the same as package level GetSubscription, but using this
// wallet's SeedRPCServerAddr.
func (w *Wallet) GetSubscription(topic string, subscriber string) (*Subscription, error) {
	return GetSubscription(topic, subscriber, w.config)
}

// GetSubscribersCount is the same as package level GetSubscribersCount, but
// this wallet's SeedRPCServerAddr.
func (w *Wallet) GetSubscribersCount(topic string) (int, error) {
	return GetSubscribersCount(topic, w.config)
}

// GetRegistrant is the same as package level GetRegistrant, but this wallet's
// SeedRPCServerAddr.
func (w *Wallet) GetRegistrant(name string) (*Registrant, error) {
	return GetRegistrant(name, w.config)
}

// SendRawTransaction is the same as package level SendRawTransaction, but using
// this wallet's SeedRPCServerAddr.
func (w *Wallet) SendRawTransaction(txn *transaction.Transaction) (string, error) {
	return SendRawTransaction(txn, w.config)
}

// Transfer is a shortcut for Transfer using this wallet as SignerRPCClient.
func (w *Wallet) Transfer(address, amount string, config *TransactionConfig) (string, error) {
	return Transfer(w, address, amount, config)
}

// RegisterName is a shortcut for RegisterName using this wallet as
// SignerRPCClient.
func (w *Wallet) RegisterName(name string, config *TransactionConfig) (string, error) {
	return RegisterName(w, name, config)
}

// TransferName is a shortcut for TransferName using this wallet as
// SignerRPCClient.
func (w *Wallet) TransferName(name string, recipientPubKey []byte, config *TransactionConfig) (string, error) {
	return TransferName(w, name, recipientPubKey, config)
}

// DeleteName is a shortcut for DeleteName using this wallet as SignerRPCClient.
func (w *Wallet) DeleteName(name string, config *TransactionConfig) (string, error) {
	return DeleteName(w, name, config)
}

// Subscribe is a shortcut for Subscribe using this wallet as SignerRPCClient.
//
// Duration is changed to signed int for gomobile compatibility.
func (w *Wallet) Subscribe(identifier, topic string, duration int, meta string, config *TransactionConfig) (string, error) {
	return Subscribe(w, identifier, topic, duration, meta, config)
}

// Unsubscribe is a shortcut for Unsubscribe using this wallet as
// SignerRPCClient.
func (w *Wallet) Unsubscribe(identifier, topic string, config *TransactionConfig) (string, error) {
	return Unsubscribe(w, identifier, topic, config)
}
