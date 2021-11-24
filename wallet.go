package nkn

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"sync"

	"github.com/nknorg/nkn/v2/common"
	"github.com/nknorg/nkn/v2/pb"
	"github.com/nknorg/nkn/v2/program"
	"github.com/nknorg/nkn/v2/signature"
	"github.com/nknorg/nkn/v2/transaction"
	"github.com/nknorg/nkn/v2/vault"
)

// Wallet manages assets, query state from blockchain, and send transactions to
// blockchain.
type Wallet struct {
	config  *WalletConfig
	account *Account
	address string

	lock       sync.Mutex
	walletData *vault.WalletData
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

	var walletData *vault.WalletData
	if len(config.Password) > 0 || len(config.MasterKey) > 0 {
		defer func() {
			config.Password = ""
			config.MasterKey = nil
		}()

		walletData, err = vault.NewWalletData(
			account.Account,
			[]byte(config.Password),
			config.MasterKey,
			config.IV,
			config.ScryptConfig.Salt,
			config.ScryptConfig.N,
			config.ScryptConfig.R,
			config.ScryptConfig.P,
		)
		if err != nil {
			return nil, err
		}
	}

	wallet := &Wallet{
		config:     config,
		account:    account,
		address:    account.WalletAddress(),
		walletData: walletData,
	}

	return wallet, nil
}

// WalletFromJSON recovers a wallet from wallet JSON and wallet config. The
// password in config must match the password used to create the wallet.
func WalletFromJSON(walletJSON string, config *WalletConfig) (*Wallet, error) {
	config, err := MergeWalletConfig(config)
	if err != nil {
		return nil, err
	}

	defer func() {
		config.Password = ""
		config.MasterKey = nil
	}()

	walletData := &vault.WalletData{}
	err = json.Unmarshal([]byte(walletJSON), walletData)
	if err != nil {
		return nil, err
	}

	if walletData.Version < vault.MinCompatibleWalletVersion || walletData.Version > vault.MaxCompatibleWalletVersion {
		return nil, ErrInvalidWalletVersion
	}

	account, err := walletData.DecryptAccount([]byte(config.Password))
	if err != nil {
		return nil, err
	}

	address, err := account.ProgramHash.ToAddress()
	if err != nil {
		return nil, err
	}

	if address != walletData.Address {
		return nil, ErrWrongPassword
	}

	if walletData.Version == vault.WalletVersion {
		wallet := &Wallet{
			config:     config,
			account:    &Account{account},
			address:    address,
			walletData: walletData,
		}
		return wallet, nil
	}

	iv, err := hex.DecodeString(walletData.IV)
	if err != nil {
		return nil, err
	}

	masterKey, err := walletData.DecryptMasterKey([]byte(config.Password))
	if err != nil {
		return nil, err
	}

	config.IV = iv
	config.MasterKey = masterKey

	return NewWallet(&Account{account}, config)
}

func (w *Wallet) getWalletData() (*vault.WalletData, error) {
	w.lock.Lock()
	defer w.lock.Unlock()
	if w.walletData == nil {
		walletData, err := vault.NewWalletData(
			w.account.Account,
			[]byte(w.config.Password),
			w.config.MasterKey,
			w.config.IV,
			w.config.ScryptConfig.Salt,
			w.config.ScryptConfig.N,
			w.config.ScryptConfig.R,
			w.config.ScryptConfig.P,
		)
		if err != nil {
			return nil, err
		}
		w.walletData = walletData
	}
	return w.walletData, nil
}

// MarshalJSON serialize the wallet to JSON string encrypted by password used to
// create the wallet. The same password must be used to recover the wallet from
// JSON string.
func (w *Wallet) MarshalJSON() ([]byte, error) {
	walletData, err := w.getWalletData()
	if err != nil {
		return nil, err
	}
	return json.Marshal(walletData)
}

// ToJSON is a shortcut for wallet.MarshalJSON, but returns string instead of
// bytes.
func (w *Wallet) ToJSON() (string, error) {
	b, err := w.MarshalJSON()
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Account returns the account of the wallet.
func (w *Wallet) Account() *Account {
	return w.account
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

// VerifyPassword returns nil if provided password is the correct password of
// this wallet.
func (w *Wallet) VerifyPassword(password string) error {
	walletData, err := w.getWalletData()
	if err != nil {
		return err
	}
	return walletData.VerifyPassword([]byte(password))
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
func (w *Wallet) NewNanoPayClaimer(recipientAddress string, claimIntervalMs, lingerMs int32, minFlushAmount string, onError *OnError) (*NanoPayClaimer, error) {
	if len(recipientAddress) == 0 {
		recipientAddress = w.Address()
	}
	return NewNanoPayClaimer(w, recipientAddress, claimIntervalMs, lingerMs, minFlushAmount, onError)
}

// GetNonce wraps GetNonceContext with background context.
func (w *Wallet) GetNonce(txPool bool) (int64, error) {
	return w.GetNonceContext(context.Background(), txPool)
}

// GetNonceContext is the same as package level GetNonceContext, but using this
// wallet's SeedRPCServerAddr.
func (w *Wallet) GetNonceContext(ctx context.Context, txPool bool) (int64, error) {
	return w.GetNonceByAddress(w.address, txPool)
}

// GetNonceByAddress wraps GetNonceByAddressContext with background context.
func (w *Wallet) GetNonceByAddress(address string, txPool bool) (int64, error) {
	return w.GetNonceByAddressContext(context.Background(), address, txPool)
}

// GetNonceByAddressContext is the same as package level GetNonceContext, but
// using this wallet's SeedRPCServerAddr.
func (w *Wallet) GetNonceByAddressContext(ctx context.Context, address string, txPool bool) (int64, error) {
	return GetNonce(address, txPool, w.config)
}

// GetHeight wraps GetHeightContext with background context.
func (w *Wallet) GetHeight() (int32, error) {
	return w.GetHeightContext(context.Background())
}

// GetHeightContext is the same as package level GetHeightContext, but using
// this wallet's SeedRPCServerAddr.
func (w *Wallet) GetHeightContext(ctx context.Context) (int32, error) {
	return GetHeight(w.config)
}

// Balance wraps BalanceContext with background context.
func (w *Wallet) Balance() (*Amount, error) {
	return w.BalanceContext(context.Background())
}

// BalanceContext is the same as package level GetBalanceContext, but using this
// wallet's SeedRPCServerAddr.
func (w *Wallet) BalanceContext(ctx context.Context) (*Amount, error) {
	return w.BalanceByAddress(w.address)
}

// BalanceByAddress wraps BalanceByAddressContext with background context.
func (w *Wallet) BalanceByAddress(address string) (*Amount, error) {
	return w.BalanceByAddressContext(context.Background(), address)
}

// BalanceByAddressContext is the same as package level GetBalanceContext, but
// using this wallet's SeedRPCServerAddr.
func (w *Wallet) BalanceByAddressContext(ctx context.Context, address string) (*Amount, error) {
	return GetBalance(address, w.config)
}

// GetSubscribers wraps GetSubscribersContext with background context.
func (w *Wallet) GetSubscribers(topic string, offset, limit int, meta, txPool bool, subscriberHashPrefix []byte) (*Subscribers, error) {
	return w.GetSubscribersContext(context.Background(), topic, offset, limit, meta, txPool, subscriberHashPrefix)
}

// GetSubscribersContext is the same as package level GetSubscribersContext, but
// using this wallet's SeedRPCServerAddr.
func (w *Wallet) GetSubscribersContext(ctx context.Context, topic string, offset, limit int, meta, txPool bool, subscriberHashPrefix []byte) (*Subscribers, error) {
	return GetSubscribers(topic, offset, limit, meta, txPool, subscriberHashPrefix, w.config)
}

// GetSubscription wraps GetSubscriptionContext with background context.
func (w *Wallet) GetSubscription(topic string, subscriber string) (*Subscription, error) {
	return w.GetSubscriptionContext(context.Background(), topic, subscriber)
}

// GetSubscriptionContext is the same as package level GetSubscriptionContext,
// but using this wallet's SeedRPCServerAddr.
func (w *Wallet) GetSubscriptionContext(ctx context.Context, topic string, subscriber string) (*Subscription, error) {
	return GetSubscription(topic, subscriber, w.config)
}

// GetSubscribersCount wraps GetSubscribersCountContext with background context.
func (w *Wallet) GetSubscribersCount(topic string, subscriberHashPrefix []byte) (int, error) {
	return w.GetSubscribersCountContext(context.Background(), topic, subscriberHashPrefix)
}

// GetSubscribersCountContext is the same as package level
// GetSubscribersCountContext, but this wallet's SeedRPCServerAddr.
func (w *Wallet) GetSubscribersCountContext(ctx context.Context, topic string, subscriberHashPrefix []byte) (int, error) {
	return GetSubscribersCount(topic, subscriberHashPrefix, w.config)
}

// GetRegistrant wraps GetRegistrantContext with background context.
func (w *Wallet) GetRegistrant(name string) (*Registrant, error) {
	return w.GetRegistrantContext(context.Background(), name)
}

// GetRegistrantContext is the same as package level GetRegistrantContext, but
// this wallet's SeedRPCServerAddr.
func (w *Wallet) GetRegistrantContext(ctx context.Context, name string) (*Registrant, error) {
	return GetRegistrant(name, w.config)
}

// SendRawTransaction wraps SendRawTransactionContext with background context.
func (w *Wallet) SendRawTransaction(txn *transaction.Transaction) (string, error) {
	return w.SendRawTransactionContext(context.Background(), txn)
}

// SendRawTransactionContext is the same as package level
// SendRawTransactionContext, but using this wallet's SeedRPCServerAddr.
func (w *Wallet) SendRawTransactionContext(ctx context.Context, txn *transaction.Transaction) (string, error) {
	return SendRawTransaction(txn, w.config)
}

// Transfer wraps TransferContext with background context.
func (w *Wallet) Transfer(address, amount string, config *TransactionConfig) (string, error) {
	return w.TransferContext(context.Background(), address, amount, config)
}

// TransferContext is a shortcut for TransferContext using this wallet as
// SignerRPCClient.
func (w *Wallet) TransferContext(ctx context.Context, address, amount string, config *TransactionConfig) (string, error) {
	return Transfer(w, address, amount, config)
}

// RegisterName wraps RegisterNameContext with background context.
func (w *Wallet) RegisterName(name string, config *TransactionConfig) (string, error) {
	return w.RegisterNameContext(context.Background(), name, config)
}

// RegisterNameContext is a shortcut for RegisterNameContext using this wallet
// as SignerRPCClient.
func (w *Wallet) RegisterNameContext(ctx context.Context, name string, config *TransactionConfig) (string, error) {
	return RegisterName(w, name, config)
}

// TransferName wraps TransferNameContext with background context.
func (w *Wallet) TransferName(name string, recipientPubKey []byte, config *TransactionConfig) (string, error) {
	return w.TransferNameContext(context.Background(), name, recipientPubKey, config)
}

// TransferNameContext is a shortcut for TransferNameContext using this wallet
// as SignerRPCClient.
func (w *Wallet) TransferNameContext(ctx context.Context, name string, recipientPubKey []byte, config *TransactionConfig) (string, error) {
	return TransferName(w, name, recipientPubKey, config)
}

// DeleteName wraps DeleteNameContext with background context.
func (w *Wallet) DeleteName(name string, config *TransactionConfig) (string, error) {
	return w.DeleteNameContext(context.Background(), name, config)
}

// DeleteNameContext is a shortcut for DeleteNameContext using this wallet as
// SignerRPCClient.
func (w *Wallet) DeleteNameContext(ctx context.Context, name string, config *TransactionConfig) (string, error) {
	return DeleteName(w, name, config)
}

// Subscribe wraps SubscribeContext with background context.
func (w *Wallet) Subscribe(identifier, topic string, duration int, meta string, config *TransactionConfig) (string, error) {
	return w.SubscribeContext(context.Background(), identifier, topic, duration, meta, config)
}

// SubscribeContext is a shortcut for SubscribeContext using this wallet as
// SignerRPCClient.
//
// Duration is changed to signed int for gomobile compatibility.
func (w *Wallet) SubscribeContext(ctx context.Context, identifier, topic string, duration int, meta string, config *TransactionConfig) (string, error) {
	return Subscribe(w, identifier, topic, duration, meta, config)
}

// Unsubscribe wraps UnsubscribeContext with background context.
func (w *Wallet) Unsubscribe(identifier, topic string, config *TransactionConfig) (string, error) {
	return w.UnsubscribeContext(context.Background(), identifier, topic, config)
}

// UnsubscribeContext is a shortcut for UnsubscribeContext using this wallet as
// SignerRPCClient.
func (w *Wallet) UnsubscribeContext(ctx context.Context, identifier, topic string, config *TransactionConfig) (string, error) {
	return Unsubscribe(w, identifier, topic, config)
}
