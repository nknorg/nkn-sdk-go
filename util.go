package nkn

import (
	"crypto/rand"
	"crypto/sha256"
	"log"
	"math/big"
	"strings"
	"time"

	"github.com/nknorg/nkn/common"
	"github.com/nknorg/nkn/crypto"
	"github.com/nknorg/nkn/program"
	"github.com/nknorg/nkn/util/address"
	"github.com/nknorg/nkn/vault"
)

const (
	// AmountUnit is the inverse of the NKN precision
	AmountUnit = common.StorageFactor
)

var (
	zeroTime time.Time
)

// Account is a wrapper type for gomobile compatibility.
type Account struct{ *vault.Account }

// NewAccount creates an account from secret seed. Seed length should be 32 or
// 0. If seed has zero length (including nil), a random seed will be generated.
func NewAccount(seed []byte) (*Account, error) {
	if len(seed) == 0 {
		account, err := vault.NewAccount()
		return &Account{account}, err
	}

	err := crypto.CheckSeed(seed)
	if err != nil {
		return nil, err
	}

	privateKey := crypto.GetPrivateKeyFromSeed(seed)

	account, err := vault.NewAccountWithPrivatekey(privateKey)
	if err != nil {
		return nil, err
	}

	_, err = account.ProgramHash.ToAddress()
	if err != nil {
		return nil, err
	}

	return &Account{account}, err
}

// Seed returns the secret seed of the account. Secret seed can be used to
// create client/wallet with the same key pair and should be kept secret and
// safe.
func (account *Account) Seed() []byte {
	return crypto.GetSeedFromPrivateKey(account.PrivKey())
}

// PubKey returns the public key of the account.
func (account *Account) PubKey() []byte {
	return account.Account.PublicKey.EncodePoint()
}

// WalletAddress returns the wallet address of the account.
func (account *Account) WalletAddress() string {
	// no need to handle error here as it's already checked in NewAccount()
	addr, _ := account.ProgramHash.ToAddress()
	return addr
}

// Amount is a wrapper type for gomobile compatibility.
type Amount struct{ common.Fixed64 }

// NewAmount creates an amount from string in unit of NKN. For example, "0.1"
// will be parsed as 0.1 NKN.
func NewAmount(s string) (*Amount, error) {
	fixed64, err := common.StringToFixed64(s)
	if err != nil {
		return nil, err
	}
	return &Amount{fixed64}, err
}

// ToFixed64 returns amount as Fixed64 type.
func (amount *Amount) ToFixed64() common.Fixed64 {
	if amount == nil {
		return 0
	}
	return amount.Fixed64
}

// StringArray is a wrapper type for gomobile compatibility. StringArray is not
// protected by lock and should not be read and write at the same time.
type StringArray struct{ Elems []string }

// NewStringArray creates a StringArray from a list of string elements.
func NewStringArray(elems ...string) *StringArray {
	return &StringArray{elems}
}

// NewStringArrayFromString creates a StringArray from a single string input.
// The input string will be split to string array by whitespace.
func NewStringArrayFromString(s string) *StringArray {
	return &StringArray{strings.Fields(s)}
}

// Len returns the string array length.
func (sa *StringArray) Len() int {
	return len(sa.Elems)
}

// Append adds an element to the string array.
func (sa *StringArray) Append(s string) {
	sa.Elems = append(sa.Elems, s)
}

// StringMapFunc is a wrapper type for gomobile compatibility.
type StringMapFunc interface{ OnVisit(string, string) bool }

// StringMap is a wrapper type for gomobile compatibility. StringMap is not
// protected by lock and should not be read and write at the same time.
type StringMap struct{ Map map[string]string }

// NewStringMap creates a StringMap from a map.
func NewStringMap(m map[string]string) *StringMap {
	return &StringMap{m}
}

// NewStringMapWithSize creates an empty StringMap with a given size.
func NewStringMapWithSize(size int) *StringMap {
	return &StringMap{make(map[string]string, size)}
}

// Get returns the value of a key, or ErrKeyNotInMap if key does not exist.
func (sm *StringMap) Get(key string) (string, error) {
	if value, ok := sm.Map[key]; ok {
		return value, nil
	}
	return "", ErrKeyNotInMap
}

// Set sets the value of a key to a value.
func (sm *StringMap) Set(key, value string) {
	sm.Map[key] = value
}

// Delete deletes a key and its value from the map.
func (sm *StringMap) Delete(key string) {
	delete(sm.Map, key)
}

// Len returns the number of elements in the map.
func (sm *StringMap) Len() int {
	return len(sm.Map)
}

// Range iterates over the StringMap and call the OnVisit callback function with
// each element in the map. If the OnVisit function returns false, the iterator
// will stop and no longer visit the rest elements.
func (sm *StringMap) Range(cb StringMapFunc) {
	if cb != nil {
		for key, value := range sm.Map {
			if !cb.OnVisit(key, value) {
				return
			}
		}
	}
}

// Subscribers is a wrapper type for gomobile compatibility.
type Subscribers struct{ Subscribers, SubscribersInTxPool *StringMap }

// OnConnectFunc is a wrapper type for gomobile compatibility.
type OnConnectFunc interface{ OnConnect(*Node) }

// OnConnect is a wrapper type for gomobile compatibility.
type OnConnect struct {
	C        chan *Node
	Callback OnConnectFunc
}

// NewOnConnect creates an OnConnect channel with a channel size and callback
// function.
func NewOnConnect(size int, cb OnConnectFunc) *OnConnect {
	return &OnConnect{
		C:        make(chan *Node, size),
		Callback: cb,
	}
}

// Next waits and returns the next element from the channel.
func (c *OnConnect) Next() *Node {
	return <-c.C
}

func (c *OnConnect) receive(node *Node) {
	if c.Callback != nil {
		c.Callback.OnConnect(node)
	} else {
		select {
		case c.C <- node:
		default:
		}
	}
}

// OnMessageFunc is a wrapper type for gomobile compatibility.
type OnMessageFunc interface{ OnMessage(*Message) }

// OnMessage is a wrapper type for gomobile compatibility.
type OnMessage struct {
	C        chan *Message
	Callback OnMessageFunc
}

// NewOnMessage creates an OnMessage channel with a channel size and callback
// function.
func NewOnMessage(size int, cb OnMessageFunc) *OnMessage {
	return &OnMessage{
		C:        make(chan *Message, size),
		Callback: cb,
	}
}

// Next waits and returns the next element from the channel.
func (c *OnMessage) Next() *Message {
	return <-c.C
}

func (c *OnMessage) receive(msg *Message, verbose bool) {
	if c.Callback != nil {
		c.Callback.OnMessage(msg)
	} else {
		select {
		case c.C <- msg:
		default:
			if verbose {
				log.Println("Message channel full, discarding msg...")
			}
		}
	}
}

// OnErrorFunc is a wrapper type for gomobile compatibility.
type OnErrorFunc interface{ OnError(error) }

// OnError is a wrapper type for gomobile compatibility.
type OnError struct {
	C        chan error
	Callback OnErrorFunc
}

// NewOnError creates an OnError channel with a channel size and callback
// function.
func NewOnError(size int, cb OnErrorFunc) *OnError {
	return &OnError{
		C:        make(chan error, size),
		Callback: cb,
	}
}

// Next waits and returns the next element from the channel.
func (c *OnError) Next() error {
	return <-c.C
}

func (c *OnError) receive(err error) {
	if c.Callback != nil {
		c.Callback.OnError(err)
	} else {
		select {
		case c.C <- err:
		default:
			log.Printf("OnError channel full, print error instead: %v", err)
		}
	}
}

// ClientAddr represents NKN client address. It implements net.Addr interface.
type ClientAddr struct {
	addr string
}

// NewClientAddr creates a ClientAddr from a client address string.
func NewClientAddr(addr string) *ClientAddr {
	return &ClientAddr{addr: addr}
}

// Network returns "nkn"
func (addr ClientAddr) Network() string { return "nkn" }

// String returns the NKN client address string.
func (addr ClientAddr) String() string { return addr.addr }

func addIdentifierPrefix(base, prefix string) string {
	if len(base) == 0 {
		return prefix
	}
	if len(prefix) == 0 {
		return base
	}
	return prefix + "." + base
}

// RandomBytes return cryptographically secure random bytes with given size.
func RandomBytes(numBytes int) ([]byte, error) {
	b := make([]byte, numBytes)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

func sessionKey(remoteAddr string, sessionID []byte) string {
	return remoteAddr + string(sessionID)
}

func randUint32() uint32 {
	max := big.NewInt(4294967296)
	for {
		result, err := rand.Int(rand.Reader, max)
		if err != nil {
			continue
		}
		return uint32(result.Uint64())
	}
}

func randUint64() uint64 {
	max := new(big.Int).SetUint64(18446744073709551615)
	max.Add(max, big.NewInt(1))
	for {
		result, err := rand.Int(rand.Reader, max)
		if err != nil {
			continue
		}
		return result.Uint64()
	}
}

func addressToID(addr string) []byte {
	id := sha256.Sum256([]byte(addr))
	return id[:]
}

// ClientAddrToPubKey converts a NKN client address to its public key.
func ClientAddrToPubKey(clientAddr string) ([]byte, error) {
	_, pk, _, err := address.ParseClientAddress(clientAddr)
	if err != nil {
		return nil, err
	}
	_, err = crypto.DecodePoint(pk)
	if err != nil {
		return nil, err
	}
	return pk, nil
}

// PubKeyToWalletAddr converts a public key to its NKN wallet address.
func PubKeyToWalletAddr(pk []byte) (string, error) {
	pubKey, err := crypto.DecodePoint(pk)
	if err != nil {
		return "", err
	}
	programHash, err := program.CreateProgramHash(pubKey)
	if err != nil {
		return "", err
	}
	return programHash.ToAddress()
}

// ClientAddrToWalletAddr converts a NKN client address to its NKN wallet
// address. It's a shortcut for calling ClientAddrToPubKey followed by
// PubKeyToWalletAddr.
func ClientAddrToWalletAddr(clientAddr string) (string, error) {
	_, pk, _, err := address.ParseClientAddress(clientAddr)
	if err != nil {
		return "", err
	}
	return PubKeyToWalletAddr(pk)
}

// VerifyWalletAddress returns error if the given wallet address is invalid.
func VerifyWalletAddress(address string) error {
	_, err := common.ToScriptHash(address)
	return err
}
