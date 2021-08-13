package nkn

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"log"
	"math/big"
	mathRand "math/rand"
	"strings"
	"sync"
	"time"

	"github.com/nknorg/nkn/v2/common"
	"github.com/nknorg/nkn/v2/crypto"
	"github.com/nknorg/nkn/v2/pb"
	"github.com/nknorg/nkn/v2/program"
	"github.com/nknorg/nkn/v2/util/address"
	"github.com/nknorg/nkn/v2/vault"
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
	var account *vault.Account
	var err error
	if len(seed) > 0 {
		account, err = vault.NewAccountWithSeed(seed)
	} else {
		account, err = vault.NewAccount()
	}
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
	return account.Account.PublicKey
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
type StringArray struct{ elems []string }

// NewStringArray creates a StringArray from a list of string elements.
func NewStringArray(elems ...string) *StringArray {
	return &StringArray{elems}
}

// NewStringArrayFromString creates a StringArray from a single string input.
// The input string will be split to string array by whitespace.
func NewStringArrayFromString(s string) *StringArray {
	return &StringArray{strings.Fields(s)}
}

// Elems returns the string array elements.
func (sa *StringArray) Elems() []string {
	if sa == nil {
		return nil
	}
	return sa.elems
}

// Len returns the string array length.
func (sa *StringArray) Len() int {
	return len(sa.Elems())
}

// Append adds an element to the string array.
func (sa *StringArray) Append(s string) {
	sa.elems = append(sa.elems, s)
}

// RandomElem returns a randome element from the string array. The random number
// is generated using math/rand and thus not cryptographically secure.
func (sa *StringArray) RandomElem() string {
	if sa.Len() == 0 {
		return ""
	}
	return sa.Elems()[mathRand.Intn(sa.Len())]
}

// StringArray returns a single string by concatenates the elements
func (sa *StringArray) Join(separator string) string {
	if sa == nil {
		return ""
	}
	return strings.Join(sa.elems, separator)
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

func (c *OnConnect) close() {
	close(c.C)
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

// NextWithTimeout waits and returns the next element from the channel, timeout in millisecond.
func (c *OnMessage) NextWithTimeout(timeout int32) *Message {
	if timeout == 0 {
		return <-c.C
	}
	select {
	case msg := <-c.C:
		return msg
	case <-time.After(time.Duration(timeout) * time.Millisecond):
		return nil
	}
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

func (c *OnMessage) close() {
	close(c.C)
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

func (c *OnError) close() {
	close(c.C)
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
	err = crypto.CheckPublicKey(pk)
	if err != nil {
		return nil, err
	}
	return pk, nil
}

// PubKeyToWalletAddr converts a public key to its NKN wallet address.
func PubKeyToWalletAddr(pubKey []byte) (string, error) {
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
	pk, err := ClientAddrToPubKey(clientAddr)
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

// MeasureSeedRPCServer wraps MeasureSeedRPCServerContext with background
// context.
func MeasureSeedRPCServer(seedRPCList *StringArray, timeout int32) (*StringArray, error) {
	return MeasureSeedRPCServerContext(context.Background(), seedRPCList, timeout)
}

// MeasureSeedRPCServerContext measures the latency to seed rpc node list, only
// select the ones in persist finished state, and sort them by latency (from low
// to high). If none of the given seed rpc node is accessable or in persist
// finished state, returned string array will contain zero elements. Timeout is
// in millisecond.
func MeasureSeedRPCServerContext(ctx context.Context, seedRPCList *StringArray, timeout int32) (*StringArray, error) {
	var wg sync.WaitGroup
	var lock sync.Mutex
	rpcAddrs := make([]string, 0, seedRPCList.Len())

	for _, node := range seedRPCList.Elems() {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			nodeState, err := GetNodeStateContext(ctx, &RPCConfig{
				SeedRPCServerAddr: NewStringArray(addr),
				RPCTimeout:        timeout,
			})
			if err != nil {
				return
			}
			if nodeState.SyncState != pb.SyncState_name[int32(pb.SyncState_PERSIST_FINISHED)] {
				return
			}
			lock.Lock()
			rpcAddrs = append(rpcAddrs, addr)
			lock.Unlock()
		}(node)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-done:
	}

	return NewStringArray(rpcAddrs...), nil
}
