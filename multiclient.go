package nkn

import (
	"context"
	"log"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	multierror "github.com/hashicorp/go-multierror"
	ncp "github.com/nknorg/ncp-go"
	"github.com/nknorg/nkn-sdk-go/payloads"
	"github.com/nknorg/nkn/transaction"
	"github.com/nknorg/nkn/util/address"
	"github.com/patrickmn/go-cache"
)

const (
	// MultiClientIdentifierRe is the regular expression to check whether an
	// identifier is a multiclient protocol identifier.
	MultiClientIdentifierRe = "^__\\d+__$"

	// DefaultSessionAllowAddr is the default session allow address if none is
	// provided when calling listen.
	DefaultSessionAllowAddr = ".*"

	// SessionIDSize is the default session id size in bytes.
	SessionIDSize = MessageIDSize

	// Channel length for session that is received but not accepted by user.
	acceptSessionBufSize = 128
)

var (
	multiClientIdentifierRe = regexp.MustCompile(MultiClientIdentifierRe)
)

func addMultiClientPrefix(dest []string, clientID int) []string {
	result := make([]string, len(dest))
	for i, addr := range dest {
		result[i] = addIdentifier(addr, clientID)
	}
	return result
}

func addIdentifier(addr string, id int) string {
	if id < 0 {
		return addr
	}
	return addIdentifierPrefix(addr, "__"+strconv.Itoa(id)+"__")
}

func removeIdentifier(src string) (string, string) {
	s := strings.SplitN(src, ".", 2)
	if len(s) > 1 {
		if multiClientIdentifierRe.MatchString(s[0]) {
			return s[1], s[0]
		}
	}
	return src, ""
}

// MultiClient sends and receives data using multiple NKN clients concurrently
// to improve reliability and latency. In addition, it supports session mode, a
// reliable streaming protocol similar to TCP based on ncp
// (https://github.com/nknorg/ncp-go).
type MultiClient struct {
	OnConnect *OnConnect // Event emitting channel when at least one client connects to node and becomes ready to send messages. One should only use the first event of the channel.
	OnMessage *OnMessage // Event emitting channel when at least one client receives a message (not including reply or ACK).

	config        *ClientConfig
	offset        int
	addr          *ClientAddr
	acceptSession chan *ncp.Session
	onClose       chan struct{}
	msgCache      *cache.Cache

	lock          sync.RWMutex
	clients       map[int]*Client
	defaultClient *Client
	acceptAddrs   []*regexp.Regexp
	isClosed      bool
	createDone    bool

	sessionLock sync.Mutex
	sessions    map[string]*ncp.Session
}

// NewMultiClient creates a multiclient with an account, an optional identifier,
// number of sub clients to create, whether to create original client without
// identifier prefix, and a optional client config that will be applied to all
// clients created. For any zero value field in config, the default client
// config value will be used. If config is nil, the default client config will
// be used.
func NewMultiClient(account *Account, baseIdentifier string, numSubClients int, originalClient bool, config *ClientConfig) (*MultiClient, error) {
	config, err := MergeClientConfig(config)
	if err != nil {
		return nil, err
	}

	numClients := numSubClients
	offset := 0
	if originalClient {
		numClients++
		offset = 1
	}

	addr := address.MakeAddressString(account.PublicKey, baseIdentifier)

	m := &MultiClient{
		config:        config,
		offset:        offset,
		addr:          NewClientAddr(addr),
		OnConnect:     NewOnConnect(1, nil),
		OnMessage:     NewOnMessage(int(config.MsgChanLen), nil),
		acceptSession: make(chan *ncp.Session, acceptSessionBufSize),
		onClose:       make(chan struct{}, 0),
		msgCache:      cache.New(time.Duration(config.MsgCacheExpiration)*time.Millisecond, time.Duration(config.MsgCacheExpiration)*time.Millisecond),
		clients:       make(map[int]*Client, numClients),
		defaultClient: nil,
		sessions:      make(map[string]*ncp.Session, 0),
	}

	var wg sync.WaitGroup
	defaultClientIdx := numSubClients
	success := make(chan struct{}, 1)
	fail := make(chan struct{}, 1)

	for i := -offset; i < numSubClients; i++ {
		wg.Add(1)
		go func(i int) {
			client, err := NewClient(account, addIdentifier(baseIdentifier, i), config)
			if err != nil {
				log.Println(err)
				wg.Done()
				return
			}

			m.lock.Lock()
			m.clients[i] = client
			if i < defaultClientIdx {
				m.defaultClient = client
				defaultClientIdx = i
			}
			m.lock.Unlock()

			select {
			case success <- struct{}{}:
			default:
			}

			wg.Done()

			node, ok := <-client.OnConnect.C
			if !ok {
				return
			}

			m.lock.RLock()
			if m.isClosed {
				m.lock.RUnlock()
				return
			}
			m.OnConnect.receive(node)
			m.lock.RUnlock()

			for {
				select {
				case msg, ok := <-client.OnMessage.C:
					if !ok {
						return
					}
					if msg.Type == SessionType {
						if !msg.Encrypted {
							continue
						}
						err := m.handleSessionMsg(addIdentifier("", i-offset), msg.Src, msg.MessageID, msg.Data)
						if err != nil {
							if err != ncp.ErrSessionClosed && err != ErrAddrNotAllowed {
								log.Println(err)
							}
							continue
						}
					} else {
						cacheKey := string(msg.MessageID)
						if _, ok := m.msgCache.Get(cacheKey); ok {
							continue
						}
						m.msgCache.Set(cacheKey, nil, cache.DefaultExpiration)

						msg.Src, _ = removeIdentifier(msg.Src)
						if msg.NoReply {
							msg.reply = func(data interface{}) error {
								return nil
							}
						} else {
							msg.reply = func(data interface{}) error {
								payload, err := newReplyPayload(data, msg.MessageID)
								if err != nil {
									return err
								}
								if err := m.send([]string{msg.Src}, payload, msg.Encrypted, 0); err != nil {
									return err
								}
								return nil
							}
						}

						m.lock.RLock()
						if m.isClosed {
							m.lock.RUnlock()
							return
						}
						m.OnMessage.receive(msg, true)
						m.lock.RUnlock()
					}
				case <-m.onClose:
					return
				}
			}
		}(i)
	}

	go func() {
		wg.Wait()
		m.lock.Lock()
		m.createDone = true
		m.lock.Unlock()
		select {
		case fail <- struct{}{}:
		default:
		}
	}()

	select {
	case <-success:
		return m, nil
	case <-fail:
		return nil, ErrCreateClientFailed
	}
}

// Seed returns the secret seed of the multiclient. Secret seed can be used to
// create client/wallet with the same key pair and should be kept secret and
// safe.
func (m *MultiClient) Seed() []byte {
	return m.GetDefaultClient().Seed()
}

// PubKey returns the public key of the multiclient.
func (m *MultiClient) PubKey() []byte {
	return m.GetDefaultClient().PubKey()
}

// Address returns the NKN client address of the multiclient. Client address is
// in the form of
//   identifier.pubKeyHex
// if identifier is not an empty string, or
//   pubKeyHex
// if identifier is an empty string.
//
// Note that client address is different from wallet address using the same key
// pair (account). Wallet address can be computed from client address, but NOT
// vice versa.
func (m *MultiClient) Address() string {
	return m.addr.String()
}

// Addr returns the NKN client address of the multiclient as net.Addr interface,
// with Network() returns "nkn" and String() returns the same value as
// multiclient.Address().
func (m *MultiClient) Addr() net.Addr {
	return m.addr
}

// GetClients returns all clients of the multiclient with client index as key.
// Subclients index starts from 0, and original client (if created) has index
// -1.
func (m *MultiClient) GetClients() map[int]*Client {
	m.lock.RLock()
	defer m.lock.RUnlock()

	if m.createDone {
		return m.clients
	}

	clients := make(map[int]*Client, len(m.clients))
	for i, client := range m.clients {
		clients[i] = client
	}
	return clients
}

// GetClient returns a client with a given index.
func (m *MultiClient) GetClient(i int) *Client {
	m.lock.RLock()
	defer m.lock.RUnlock()
	return m.clients[i]
}

// GetDefaultClient returns the default client, which is the client with
// smallest index.
func (m *MultiClient) GetDefaultClient() *Client {
	m.lock.RLock()
	defer m.lock.RUnlock()
	return m.defaultClient
}

// SendWithClient sends bytes or string data to one or multiple destinations
// using a specific client with given index.
func (m *MultiClient) SendWithClient(clientID int, dests *StringArray, data interface{}, config *MessageConfig) (*OnMessage, error) {
	client := m.GetClient(clientID)
	if client == nil {
		return nil, ErrNilClient
	}

	config, err := MergeMessageConfig(m.config.MessageConfig, config)
	if err != nil {
		return nil, err
	}

	payload, err := newMessagePayload(data, config.MessageID, config.NoReply)
	if err != nil {
		return nil, err
	}

	if err := m.sendWithClient(clientID, dests.Elems(), payload, !config.Unencrypted, config.MaxHoldingSeconds); err != nil {
		return nil, err
	}

	onReply := NewOnMessage(1, nil)
	if !config.NoReply {
		client.responseChannels.Add(string(payload.MessageId), onReply, cache.DefaultExpiration)
	}

	return onReply, nil
}

// SendBinaryWithClient is a wrapper of SendWithClient without interface type
// for gomobile compatibility.
func (m *MultiClient) SendBinaryWithClient(clientID int, dests *StringArray, data []byte, config *MessageConfig) (*OnMessage, error) {
	return m.SendWithClient(clientID, dests, data, config)
}

// SendTextWithClient is a wrapper of SendWithClient without interface type for
// gomobile compatibility.
func (m *MultiClient) SendTextWithClient(clientID int, dests *StringArray, data string, config *MessageConfig) (*OnMessage, error) {
	return m.SendWithClient(clientID, dests, data, config)
}

func (m *MultiClient) sendWithClient(clientID int, dests []string, payload *payloads.Payload, encrypted bool, maxHoldingSeconds int32) error {
	client := m.GetClient(clientID)
	if client == nil {
		return ErrNilClient
	}
	return client.send(addMultiClientPrefix(dests, clientID), payload, encrypted, maxHoldingSeconds)
}

// Send sends bytes or string data to one or multiple destinations with an
// optional config. Returned OnMessage channel will emit if a reply or ACK for
// this message is received.
func (m *MultiClient) Send(dests *StringArray, data interface{}, config *MessageConfig) (*OnMessage, error) {
	config, err := MergeMessageConfig(m.config.MessageConfig, config)
	if err != nil {
		return nil, err
	}

	payload, err := newMessagePayload(data, config.MessageID, config.NoReply)
	if err != nil {
		return nil, err
	}

	var lock sync.Mutex
	var errs error
	var onRawReply *OnMessage
	onReply := NewOnMessage(1, nil)
	clients := m.GetClients()

	if !config.NoReply {
		onRawReply = NewOnMessage(1, nil)
		// response channel is added first to prevent some client fail to handle response if send finish before receive response
		for _, client := range clients {
			client.responseChannels.Add(string(payload.MessageId), onRawReply, cache.DefaultExpiration)
		}
	}

	success := make(chan struct{}, 1)
	fail := make(chan struct{}, 1)

	go func() {
		sent := 0
		for clientID := range clients {
			err := m.sendWithClient(clientID, dests.Elems(), payload, !config.Unencrypted, config.MaxHoldingSeconds)
			if err == nil {
				select {
				case success <- struct{}{}:
				default:
				}
				sent++
			} else {
				lock.Lock()
				errs = multierror.Append(errs, err)
				lock.Unlock()
			}
		}
		if sent == 0 {
			select {
			case fail <- struct{}{}:
			default:
			}
		}

		if !config.NoReply {
			msg := <-onRawReply.C
			msg.Src, _ = removeIdentifier(msg.Src)
			onReply.receive(msg, false)
		}
	}()

	select {
	case <-success:
		return onReply, nil
	case <-fail:
		return nil, errs
	}
}

// SendBinary is a wrapper of Send without interface type for gomobile
// compatibility.
func (m *MultiClient) SendBinary(dests *StringArray, data []byte, config *MessageConfig) (*OnMessage, error) {
	return m.Send(dests, data, config)
}

// SendText is a wrapper of Send without interface type for gomobile
// compatibility.
func (m *MultiClient) SendText(dests *StringArray, data string, config *MessageConfig) (*OnMessage, error) {
	return m.Send(dests, data, config)
}

func (m *MultiClient) send(dests []string, payload *payloads.Payload, encrypted bool, maxHoldingSeconds int32) error {
	var lock sync.Mutex
	var errs error
	success := make(chan struct{}, 1)
	fail := make(chan struct{}, 1)
	go func() {
		sent := 0
		for clientID := range m.GetClients() {
			err := m.sendWithClient(clientID, dests, payload, encrypted, maxHoldingSeconds)
			if err == nil {
				select {
				case success <- struct{}{}:
				default:
				}
				sent++
			} else {
				lock.Lock()
				errs = multierror.Append(errs, err)
				lock.Unlock()
			}
		}
		if sent == 0 {
			select {
			case fail <- struct{}{}:
			default:
			}
		}
	}()

	select {
	case <-success:
		return nil
	case <-fail:
		return errs
	}
}

// Publish sends bytes or string data to all subscribers of a topic with an
// optional config.
func (m *MultiClient) Publish(topic string, data interface{}, config *MessageConfig) error {
	return publish(m, topic, data, config)
}

// PublishBinary is a wrapper of Publish without interface type for gomobile
// compatibility.
func (m *MultiClient) PublishBinary(topic string, data []byte, config *MessageConfig) error {
	return m.Publish(topic, data, config)
}

// PublishText is a wrapper of Publish without interface type for gomobile
// compatibility.
func (m *MultiClient) PublishText(topic string, data string, config *MessageConfig) error {
	return m.Publish(topic, data, config)
}

func (m *MultiClient) newSession(remoteAddr string, sessionID []byte, config *ncp.Config) (*ncp.Session, error) {
	rawClients := m.GetClients()
	clientIDs := make([]string, 0, len(rawClients))
	clients := make(map[string]*Client, len(rawClients))
	for id, client := range rawClients {
		clientID := addIdentifier("", id)
		clientIDs = append(clientIDs, clientID)
		clients[clientID] = client
	}
	sort.Strings(clientIDs)
	return ncp.NewSession(m.addr, NewClientAddr(remoteAddr), clientIDs, nil, (func(localClientID, remoteClientID string, buf []byte, writeTimeout time.Duration) error {
		payload := &payloads.Payload{
			Type:      payloads.SESSION,
			MessageId: sessionID,
			Data:      buf,
		}
		client := clients[localClientID]
		err := client.sendTimeout([]string{addIdentifierPrefix(remoteAddr, remoteClientID)}, payload, true, 0, writeTimeout)
		if err != nil {
			return err
		}
		return nil
	}), config)
}

func (m *MultiClient) shouldAcceptAddr(addr string) bool {
	for _, allowAddr := range m.acceptAddrs {
		if allowAddr.MatchString(addr) {
			return true
		}
	}
	return false
}

func (m *MultiClient) handleSessionMsg(localClientID, src string, sessionID, data []byte) error {
	remoteAddr, remoteClientID := removeIdentifier(src)
	sessionKey := sessionKey(remoteAddr, sessionID)
	var err error

	m.sessionLock.Lock()
	session, ok := m.sessions[sessionKey]
	if !ok {
		if !m.shouldAcceptAddr(remoteAddr) {
			m.sessionLock.Unlock()
			return ErrAddrNotAllowed
		}

		session, err = m.newSession(remoteAddr, sessionID, m.config.SessionConfig)
		if err != nil {
			m.sessionLock.Unlock()
			return err
		}
		m.sessions[sessionKey] = session
	}
	m.sessionLock.Unlock()

	err = session.ReceiveWith(localClientID, remoteClientID, data)
	if err != nil {
		return err
	}

	if !ok {
		select {
		case m.acceptSession <- session:
		default:
			log.Println("Accept session channel full, discard request...")
		}
	}

	return nil
}

// Listen will make multiclient start accepting sessions from address that
// matches any of the given regular expressions. If addrsRe is nil, any address
// will be accepted. Each function call will overwrite previous listening
// addresses.
func (m *MultiClient) Listen(addrsRe *StringArray) error {
	var addrs []string
	if addrsRe == nil {
		addrs = []string{DefaultSessionAllowAddr}
	} else {
		addrs = addrsRe.Elems()
	}

	var err error
	acceptAddrs := make([]*regexp.Regexp, len(addrs))
	for i := 0; i < len(acceptAddrs); i++ {
		acceptAddrs[i], err = regexp.Compile(addrs[i])
		if err != nil {
			return err
		}
	}

	m.lock.Lock()
	m.acceptAddrs = acceptAddrs
	m.lock.Unlock()

	return nil
}

// Dial is the same as DialSession, but return type is net.Conn interface.
func (m *MultiClient) Dial(remoteAddr string) (net.Conn, error) {
	return m.DialSession(remoteAddr)
}

// DialSession dials a session to a remote client address using this
// multiclient's dial config.
func (m *MultiClient) DialSession(remoteAddr string) (*ncp.Session, error) {
	return m.DialWithConfig(remoteAddr, nil)
}

// DialWithConfig dials a session with a dial config. For any zero value field
// in config, this default dial config value of this multiclient will be used.
// If config is nil, the default dial config of this multiclient will be used.
func (m *MultiClient) DialWithConfig(remoteAddr string, config *DialConfig) (*ncp.Session, error) {
	config, err := MergeDialConfig(m.config.SessionConfig, config)
	if err != nil {
		return nil, err
	}

	sessionID, err := RandomBytes(SessionIDSize)
	if err != nil {
		return nil, err
	}
	sessionKey := sessionKey(remoteAddr, sessionID)
	session, err := m.newSession(remoteAddr, sessionID, config.SessionConfig)
	if err != nil {
		return nil, err
	}

	m.sessionLock.Lock()
	m.sessions[sessionKey] = session
	m.sessionLock.Unlock()

	ctx := context.Background()
	var cancel context.CancelFunc
	if config.DialTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(config.DialTimeout)*time.Millisecond)
		defer cancel()
	}

	err = session.Dial(ctx)
	if err != nil {
		return nil, err
	}

	return session, nil
}

// AcceptSession will wait and return the first incoming session from allowed
// remote addresses. If multiclient is closed, it will return immediately with
// ErrClosed.
func (m *MultiClient) AcceptSession() (*ncp.Session, error) {
	for {
		select {
		case session := <-m.acceptSession:
			err := session.Accept()
			if err != nil {
				log.Println(err)
				continue
			}
			return session, nil
		case <-m.onClose:
			return nil, ErrClosed
		}
	}
}

// Accept is the same as AcceptSession, but the return type is net.Conn
// interface.
func (m *MultiClient) Accept() (net.Conn, error) {
	return m.AcceptSession()
}

// Close closes the multiclient, including all clients it created and all
// sessions dialed and accepted. Calling close multiple times is allowed and
// will not have any effect.
func (m *MultiClient) Close() error {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.isClosed {
		return nil
	}

	m.sessionLock.Lock()
	for _, session := range m.sessions {
		err := session.Close()
		if err != nil {
			log.Println(err)
			continue
		}
	}
	m.sessionLock.Unlock()

	time.AfterFunc(time.Duration(m.config.SessionConfig.Linger)*time.Millisecond, func() {
		for _, client := range m.GetClients() {
			err := client.Close()
			if err != nil {
				log.Println(err)
				continue
			}
		}
	})

	m.isClosed = true

	m.OnConnect.close()
	m.OnMessage.close()

	close(m.onClose)

	return nil
}

// IsClosed returns whether this multiclient is closed.
func (m *MultiClient) IsClosed() bool {
	m.lock.RLock()
	defer m.lock.RUnlock()
	return m.isClosed
}

func (m *MultiClient) getConfig() *ClientConfig {
	return m.config
}

// SignTransaction signs an unsigned transaction using this multiclient's key
// pair.
func (m *MultiClient) SignTransaction(tx *transaction.Transaction) error {
	return m.GetDefaultClient().SignTransaction(tx)
}

// NewNanoPay is a shortcut for NewNanoPay using this multiclient's wallet
// address as sender.
//
// Duration is changed to signed int for gomobile compatibility.
func (m *MultiClient) NewNanoPay(recipientAddress, fee string, duration int) (*NanoPay, error) {
	return NewNanoPay(m, m.GetDefaultClient().wallet, recipientAddress, fee, duration)
}

// NewNanoPayClaimer is a shortcut for NewNanoPayClaimer using this multiclient
// as RPC client.
func (m *MultiClient) NewNanoPayClaimer(recipientAddress string, claimIntervalMs int32, onError *OnError) (*NanoPayClaimer, error) {
	if len(recipientAddress) == 0 {
		recipientAddress = m.GetDefaultClient().wallet.address
	}
	return NewNanoPayClaimer(m, recipientAddress, claimIntervalMs, onError)
}

// GetNonce is the same as package level GetNonce, but using connected node as
// the RPC server, followed by this multiclient's SeedRPCServerAddr if failed.
func (m *MultiClient) GetNonce(txPool bool) (int64, error) {
	return m.GetNonceByAddress(m.GetDefaultClient().wallet.address, txPool)
}

// GetNonceByAddress is the same as package level GetNonce, but using connected
// node as the RPC server, followed by this multiclient's SeedRPCServerAddr if
// failed.
func (m *MultiClient) GetNonceByAddress(address string, txPool bool) (int64, error) {
	for _, c := range m.GetClients() {
		if c.wallet.config.SeedRPCServerAddr.Len() > 0 {
			res, err := GetNonce(address, txPool, c.wallet.config)
			if err == nil {
				return res, err
			}
		}
	}
	return GetNonce(address, txPool, m.config)
}

// GetHeight is the same as package level GetHeight, but using connected node as
// the RPC server, followed by this multiclient's SeedRPCServerAddr if failed.
func (m *MultiClient) GetHeight() (int32, error) {
	for _, c := range m.GetClients() {
		if c.wallet.config.SeedRPCServerAddr.Len() > 0 {
			res, err := GetHeight(c.wallet.config)
			if err == nil {
				return res, err
			}
		}
	}
	return GetHeight(m.config)
}

// Balance is the same as package level GetBalance, but using connected node as
// the RPC server, followed by this multiclient's SeedRPCServerAddr if failed.
func (m *MultiClient) Balance() (*Amount, error) {
	return m.BalanceByAddress(m.GetDefaultClient().wallet.address)
}

// BalanceByAddress is the same as package level GetBalance, but using connected
// node as the RPC server, followed by this multiclient's SeedRPCServerAddr if
// failed.
func (m *MultiClient) BalanceByAddress(address string) (*Amount, error) {
	for _, c := range m.GetClients() {
		if c.wallet.config.SeedRPCServerAddr.Len() > 0 {
			res, err := GetBalance(address, c.wallet.config)
			if err == nil {
				return res, err
			}
		}
	}
	return GetBalance(address, m.config)
}

// GetSubscribers is the same as package level GetSubscribers, but using
// connected node as the RPC server, followed by this multiclient's
// SeedRPCServerAddr if failed.
func (m *MultiClient) GetSubscribers(topic string, offset, limit int, meta, txPool bool) (*Subscribers, error) {
	for _, c := range m.GetClients() {
		if c.wallet.config.SeedRPCServerAddr.Len() > 0 {
			res, err := GetSubscribers(topic, offset, limit, meta, txPool, c.wallet.config)
			if err == nil {
				return res, err
			}
		}
	}
	return GetSubscribers(topic, offset, limit, meta, txPool, m.config)
}

// GetSubscription is the same as package level GetSubscription, but using
// connected node as the RPC server, followed by this multiclient's
// SeedRPCServerAddr if failed.
func (m *MultiClient) GetSubscription(topic string, subscriber string) (*Subscription, error) {
	for _, c := range m.GetClients() {
		if c.wallet.config.SeedRPCServerAddr.Len() > 0 {
			res, err := GetSubscription(topic, subscriber, c.wallet.config)
			if err == nil {
				return res, err
			}
		}
	}
	return GetSubscription(topic, subscriber, m.config)
}

// GetSubscribersCount is the same as package level GetSubscribersCount, but
// using connected node as the RPC server, followed by this multiclient's
// SeedRPCServerAddr if failed.
func (m *MultiClient) GetSubscribersCount(topic string) (int, error) {
	for _, c := range m.GetClients() {
		if c.wallet.config.SeedRPCServerAddr.Len() > 0 {
			res, err := GetSubscribersCount(topic, c.wallet.config)
			if err == nil {
				return res, err
			}
		}
	}
	return GetSubscribersCount(topic, m.config)
}

// GetRegistrant is the same as package level GetRegistrant, but using connected
// node as the RPC server, followed by this multiclient's SeedRPCServerAddr if
// failed.
func (m *MultiClient) GetRegistrant(name string) (*Registrant, error) {
	for _, c := range m.GetClients() {
		if c.wallet.config.SeedRPCServerAddr.Len() > 0 {
			res, err := GetRegistrant(name, c.wallet.config)
			if err == nil {
				return res, err
			}
		}
	}
	return GetRegistrant(name, m.config)
}

// SendRawTransaction is the same as package level SendRawTransaction, but using
// connected node as the RPC server, followed by this multiclient's
// SeedRPCServerAddr if failed.
func (m *MultiClient) SendRawTransaction(txn *transaction.Transaction) (string, error) {
	success := make(chan string, 1)
	fail := make(chan struct{}, 1)

	go func() {
		sent := 0
		for _, c := range m.GetClients() {
			if c.wallet.config.SeedRPCServerAddr.Len() > 0 {
				res, err := SendRawTransaction(txn, c.wallet.config)
				if err == nil {
					select {
					case success <- res:
					default:
					}
					sent++
				}
			}
		}
		if sent == 0 {
			select {
			case fail <- struct{}{}:
			default:
			}
		}
	}()

	select {
	case res := <-success:
		return res, nil
	case <-fail:
		return SendRawTransaction(txn, m.config)
	}
}

// Transfer is a shortcut for Transfer using this multiclient as
// SignerRPCClient.
func (m *MultiClient) Transfer(address, amount string, config *TransactionConfig) (string, error) {
	return Transfer(m, address, amount, config)
}

// RegisterName is a shortcut for RegisterName using this multiclient as
// SignerRPCClient.
func (m *MultiClient) RegisterName(name string, config *TransactionConfig) (string, error) {
	return RegisterName(m, name, config)
}

// TransferName is a shortcut for TransferName using this multiclient as
// SignerRPCClient.
func (m *MultiClient) TransferName(name string, recipientPubKey []byte, config *TransactionConfig) (string, error) {
	return TransferName(m, name, recipientPubKey, config)
}

// DeleteName is a shortcut for DeleteName using this multiclient as
// SignerRPCClient.
func (m *MultiClient) DeleteName(name string, config *TransactionConfig) (string, error) {
	return DeleteName(m, name, config)
}

// Subscribe is a shortcut for Subscribe using this multiclient as
// SignerRPCClient.
//
// Duration is changed to signed int for gomobile compatibility.
func (m *MultiClient) Subscribe(identifier, topic string, duration int, meta string, config *TransactionConfig) (string, error) {
	return Subscribe(m, identifier, topic, duration, meta, config)
}

// Unsubscribe is a shortcut for Unsubscribe using this multiclient as
// SignerRPCClient.
func (m *MultiClient) Unsubscribe(identifier, topic string, config *TransactionConfig) (string, error) {
	return Unsubscribe(m, identifier, topic, config)
}
