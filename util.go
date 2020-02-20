package nkn

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
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
	AmountUnit = common.StorageFactor
)

var (
	zeroTime              time.Time
	ErrKeyNotInMap        = errors.New("key not in map") // for gomobile
	ErrInvalidPayloadType = errors.New("invalid payload type")
)

// Type wrapper for gomobile compatibility
type (
	Account       struct{ *vault.Account }
	Amount        struct{ common.Fixed64 }
	StringArray   struct{ Elems []string }
	StringMap     struct{ Map map[string]string }
	Subscribers   struct{ Subscribers, SubscribersInTxPool *StringMap }
	StringMapFunc interface{ OnVisit(string, string) bool }
	OnConnectFunc interface{ OnConnect(*NodeInfo) }
	OnMessageFunc interface{ OnMessage(*Message) }
	OnBlockFunc   interface{ OnBlock(*BlockInfo) }
	OnErrorFunc   interface{ OnError(error) }
	OnConnect     struct {
		C        chan *NodeInfo
		Callback OnConnectFunc
	}
	OnMessage struct {
		C        chan *Message
		Callback OnMessageFunc
	}
	OnBlock struct {
		C        chan *BlockInfo
		Callback OnBlockFunc
	}
	OnError struct {
		C        chan error
		Callback OnErrorFunc
	}
)

func NewAccount(seed []byte) (*Account, error) {
	if len(seed) == 0 {
		account, err := vault.NewAccount()
		return &Account{account}, err
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

func (account *Account) Seed() []byte {
	return crypto.GetSeedFromPrivateKey(account.PrivKey())
}

func (account *Account) PubKey() []byte {
	return account.Account.PublicKey.EncodePoint()
}

func (account *Account) WalletAddress() string {
	addr, _ := account.ProgramHash.ToAddress()
	return addr
}

func NewAmount(s string) (*Amount, error) {
	fixed64, err := common.StringToFixed64(s)
	if err != nil {
		return nil, err
	}
	return &Amount{fixed64}, err
}

func (amount *Amount) ToFixed64() common.Fixed64 {
	if amount == nil {
		return 0
	}
	return amount.Fixed64
}

func NewStringArray(elems ...string) *StringArray {
	return &StringArray{elems}
}

func NewStringArrayFromString(s string) *StringArray {
	return &StringArray{strings.Fields(s)}
}

func (sa *StringArray) Len() int {
	return len(sa.Elems)
}

func (sa *StringArray) Append(s string) {
	sa.Elems = append(sa.Elems, s)
}

func NewStringMap(m map[string]string) *StringMap {
	return &StringMap{m}
}

func NewStringMapWithSize(size int) *StringMap {
	return &StringMap{make(map[string]string, size)}
}

func (sm *StringMap) Get(key string) (string, error) {
	value, ok := sm.Map[key]
	if !ok {
		return "", ErrKeyNotInMap
	}
	return value, nil
}

func (sm *StringMap) Set(key, value string) {
	sm.Map[key] = value
}

func (sm *StringMap) Delete(key string) {
	delete(sm.Map, key)
}

func (sm *StringMap) Len() int {
	return len(sm.Map)
}

func (sm *StringMap) Range(cb StringMapFunc) {
	if cb != nil {
		for key, value := range sm.Map {
			if !cb.OnVisit(key, value) {
				return
			}
		}
	}
}

func NewOnConnect(size int, cb OnConnectFunc) *OnConnect {
	return &OnConnect{
		C:        make(chan *NodeInfo, size),
		Callback: cb,
	}
}

func (c *OnConnect) Next() *NodeInfo {
	return <-c.C
}

func (c *OnConnect) receive(nodeInfo *NodeInfo) {
	if c.Callback != nil {
		c.Callback.OnConnect(nodeInfo)
	} else {
		select {
		case c.C <- nodeInfo:
		default:
		}
	}
}

func NewOnMessage(size int, cb OnMessageFunc) *OnMessage {
	return &OnMessage{
		C:        make(chan *Message, size),
		Callback: cb,
	}
}

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

func NewOnBlock(size int, cb OnBlockFunc) *OnBlock {
	return &OnBlock{
		C:        make(chan *BlockInfo, size),
		Callback: cb,
	}
}

func (c *OnBlock) Next() *BlockInfo {
	return <-c.C
}

func (c *OnBlock) receive(blockInfo *BlockInfo) {
	if c.Callback != nil {
		c.Callback.OnBlock(blockInfo)
	} else {
		select {
		case c.C <- blockInfo:
		default:
		}
	}
}

func NewOnError(size int, cb OnErrorFunc) *OnError {
	return &OnError{
		C:        make(chan error, size),
		Callback: cb,
	}
}

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

type ClientAddr struct {
	addr string
}

func NewClientAddr(addr string) *ClientAddr {
	return &ClientAddr{addr: addr}
}

func (addr ClientAddr) Network() string { return "nkn" }
func (addr ClientAddr) String() string  { return addr.addr }

func addIdentifierPrefix(base, prefix string) string {
	if len(base) == 0 {
		return prefix
	}
	if len(prefix) == 0 {
		return base
	}
	return prefix + "." + base
}

func processDest(dest []string, clientID int) []string {
	result := make([]string, len(dest))
	for i, addr := range dest {
		result[i] = addIdentifier(addr, clientID)
	}
	return result
}

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

func ClientAddrToWalletAddr(clientAddr string) (string, error) {
	_, pk, _, err := address.ParseClientAddress(clientAddr)
	if err != nil {
		return "", err
	}
	return PubKeyToWalletAddr(pk)
}

func VerifyWalletAddress(address string) error {
	_, err := common.ToScriptHash(address)
	return err
}
