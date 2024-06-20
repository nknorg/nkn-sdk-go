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
	"github.com/nknorg/nkn/v2/transaction"
	"github.com/nknorg/nkn/v2/util/address"
	"github.com/nknorg/nkngomobile"
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
	resolvers     []Resolver

	lock          sync.RWMutex
	clients       map[int]*Client
	defaultClient *Client
	acceptAddrs   []*regexp.Regexp
	isClosed      bool
	createDone    bool

	sessionLock sync.Mutex
	sessions    map[string]*ncp.Session
}

// NewMultiClientV2 creates a MultiClient with an account, an optional identifier,
// and a optional client config. For any zero value field in config, the default
// client config value will be used. If config is nil, the default client config
// will be used.
func NewMultiClientV2(account *Account, identifier string, config *ClientConfig) (*MultiClient, error) {
	config, err := MergeClientConfig(config)
	if err != nil {
		return nil, err
	}

	return NewMultiClient(account, identifier, config.MultiClientNumClients, config.MultiClientOriginalClient, config)
}

// NewMultiClient creates a multiclient with an account, an optional identifier,
// number of sub clients to create, whether to create original client without
// identifier prefix, and an optional client config that will be applied to all
// clients created. For any zero value field in config, the default client
// config value will be used. If config is nil, the default client config will
// be used.
//
// Deprecated:  Use NewMultiClientV2 instead.
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

	gomobileResolvers := config.Resolvers.Elems()
	resolvers := make([]Resolver, 0, len(gomobileResolvers))
	for i := 0; i < len(resolvers); i++ {
		r, ok := gomobileResolvers[i].(Resolver)
		if !ok {
			return nil, ErrInvalidResolver
		}
		resolvers = append(resolvers, r)
	}

	m := &MultiClient{
		config:        config,
		offset:        offset,
		addr:          NewClientAddr(addr),
		OnConnect:     NewOnConnect(1, nil),
		OnMessage:     NewOnMessage(int(config.MsgChanLen), nil),
		acceptSession: make(chan *ncp.Session, acceptSessionBufSize),
		onClose:       make(chan struct{}),
		msgCache:      cache.New(time.Duration(config.MsgCacheExpiration)*time.Millisecond, time.Duration(config.MsgCacheCleanupInterval)*time.Millisecond),
		clients:       make(map[int]*Client, numClients),
		defaultClient: nil,
		sessions:      make(map[string]*ncp.Session),
		resolvers:     resolvers,
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

			if m.isClosed {
				err := client.Close()
				if err != nil {
					log.Println(err)
				}
				wg.Done()
				m.lock.Unlock()
				return
			}

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

			go func() {
				for {
					select {
					case node, ok := <-client.OnConnect.C:
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
					case <-m.onClose:
						return
					}
				}
			}()

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
								payload, err := NewReplyPayload(data, msg.MessageID)
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

// Account returns the account of the multiclient.
func (m *MultiClient) Account() *Account {
	return m.GetDefaultClient().account
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
//
//	identifier.pubKeyHex
//
// if identifier is not an empty string, or
//
//	pubKeyHex
//
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
// the smallest index.
func (m *MultiClient) GetDefaultClient() *Client {
	m.lock.RLock()
	defer m.lock.RUnlock()
	return m.defaultClient
}

// SendWithClient sends bytes or string data to one or multiple destinations
// using a specific client with given index.
func (m *MultiClient) SendWithClient(clientID int, dests *nkngomobile.StringArray, data interface{}, config *MessageConfig) (*OnMessage, error) {
	client := m.GetClient(clientID)
	if client == nil {
		return nil, ErrNilClient
	}

	config, err := MergeMessageConfig(m.config.MessageConfig, config)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(m.config.ResolverTimeout)*time.Millisecond)
	defer cancel()

	destArr, err := m.ResolveDestsContext(ctx, dests)
	if err != nil {
		return nil, err
	}

	payload, err := newMessagePayload(data, config.MessageID, config.NoReply)
	if err != nil {
		return nil, err
	}

	if err := m.sendWithClient(clientID, destArr.Elems(), payload, !config.Unencrypted, config.MaxHoldingSeconds); err != nil {
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
func (m *MultiClient) SendBinaryWithClient(clientID int, dests *nkngomobile.StringArray, data []byte, config *MessageConfig) (*OnMessage, error) {
	return m.SendWithClient(clientID, dests, data, config)
}

// SendTextWithClient is a wrapper of SendWithClient without interface type for
// gomobile compatibility.
func (m *MultiClient) SendTextWithClient(clientID int, dests *nkngomobile.StringArray, data string, config *MessageConfig) (*OnMessage, error) {
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
func (m *MultiClient) Send(dests *nkngomobile.StringArray, data interface{}, config *MessageConfig) (*OnMessage, error) {
	config, err := MergeMessageConfig(m.config.MessageConfig, config)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(m.config.ResolverTimeout)*time.Millisecond)
	defer cancel()

	destArr, err := m.ResolveDestsContext(ctx, dests)
	if err != nil {
		return nil, err
	}

	payload, ok := data.(*payloads.Payload)
	if !ok {
		payload, err = newMessagePayload(data, config.MessageID, config.NoReply)
		if err != nil {
			return nil, err
		}
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
			err := m.sendWithClient(clientID, destArr.Elems(), payload, !config.Unencrypted, config.MaxHoldingSeconds)
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
func (m *MultiClient) SendBinary(dests *nkngomobile.StringArray, data []byte, config *MessageConfig) (*OnMessage, error) {
	return m.Send(dests, data, config)
}

// SendText is a wrapper of Send without interface type for gomobile
// compatibility.
func (m *MultiClient) SendText(dests *nkngomobile.StringArray, data string, config *MessageConfig) (*OnMessage, error) {
	return m.Send(dests, data, config)
}

// SendPayload is a wrapper of Send without interface type for gomobile
// compatibility.
func (m *MultiClient) SendPayload(dests *nkngomobile.StringArray, payload *payloads.Payload, config *MessageConfig) (*OnMessage, error) {
	return m.Send(dests, payload, config)
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

// ResolveDest wraps ResolveDestContext with background context
func (m *MultiClient) ResolveDest(dest string) (string, error) {
	return m.ResolveDestContext(context.Background(), dest)
}

// ResolveDestContext resolvers an address, returns NKN address
func (m *MultiClient) ResolveDestContext(ctx context.Context, dest string) (string, error) {
	return ResolveDestN(ctx, dest, m.resolvers, m.config.ResolverDepth)
}

// ResolveDests wraps ResolveDestsContext with background context
func (m *MultiClient) ResolveDests(dests *nkngomobile.StringArray) (*nkngomobile.StringArray, error) {
	return m.ResolveDestsContext(context.Background(), dests)
}

// ResolveDestsContext resolvers multiple addresses
func (m *MultiClient) ResolveDestsContext(ctx context.Context, dests *nkngomobile.StringArray) (*nkngomobile.StringArray, error) {
	destArr, err := ResolveDests(ctx, dests.Elems(), m.resolvers, m.config.ResolverDepth)
	if err != nil {
		return nil, err
	}
	return nkngomobile.NewStringArray(destArr...), nil
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
	return ncp.NewSession(m.addr, NewClientAddr(remoteAddr), clientIDs, nil, func(localClientID, remoteClientID string, buf []byte, writeTimeout time.Duration) error {
		payload := &payloads.Payload{
			Type:      payloads.PayloadType_SESSION,
			MessageId: sessionID,
			Data:      buf,
		}
		client := clients[localClientID]
		err := client.sendTimeout([]string{addIdentifierPrefix(remoteAddr, remoteClientID)}, payload, true, 0, writeTimeout)
		if err != nil {
			return err
		}
		return nil
	}, config)
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
func (m *MultiClient) Listen(addrsRe *nkngomobile.StringArray) error {
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

// Reconnect forces all clients to find node and connect again.
func (m *MultiClient) Reconnect() {
	m.lock.RLock()
	defer m.lock.RUnlock()

	if m.isClosed {
		return
	}

	for _, client := range m.clients {
		client.Reconnect(nil)
	}
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
func (m *MultiClient) NewNanoPayClaimer(recipientAddress string, claimIntervalMs, lingerMs int32, minFlushAmount string, onError *OnError) (*NanoPayClaimer, error) {
	if len(recipientAddress) == 0 {
		recipientAddress = m.GetDefaultClient().wallet.Address()
	}
	return NewNanoPayClaimer(m, recipientAddress, claimIntervalMs, lingerMs, minFlushAmount, onError)
}

// GetNonce wraps GetNonceContext with background context.
func (m *MultiClient) GetNonce(txPool bool) (int64, error) {
	return m.GetNonceContext(context.Background(), txPool)
}

// GetNonceContext is the same as package level GetNonceContext, but using
// connected node as the RPC server, followed by this multiclient's
// SeedRPCServerAddr if failed.
func (m *MultiClient) GetNonceContext(ctx context.Context, txPool bool) (int64, error) {
	return m.GetNonceByAddressContext(ctx, m.GetDefaultClient().wallet.Address(), txPool)
}

// GetNonceByAddress wraps GetNonceByAddressContext with background context.
func (m *MultiClient) GetNonceByAddress(address string, txPool bool) (int64, error) {
	return m.GetNonceByAddressContext(context.Background(), address, txPool)
}

// GetNonceByAddressContext is the same as package level GetNonceContext, but
// using connected node as the RPC server, followed by this multiclient's
// SeedRPCServerAddr if failed.
func (m *MultiClient) GetNonceByAddressContext(ctx context.Context, address string, txPool bool) (int64, error) {
	for _, c := range m.GetClients() {
		if c.wallet.config.SeedRPCServerAddr.Len() > 0 {
			res, err := GetNonceContext(ctx, address, txPool, c.wallet.config)
			if err == nil {
				return res, err
			}
		}
	}
	return GetNonceContext(ctx, address, txPool, m.config)
}

// GetHeight wraps GetHeightContext with background context.
func (m *MultiClient) GetHeight() (int32, error) {
	return m.GetHeightContext(context.Background())
}

// GetHeightContext is the same as package level GetHeightContext, but using
// connected node as the RPC server, followed by this multiclient's
// SeedRPCServerAddr if failed.
func (m *MultiClient) GetHeightContext(ctx context.Context) (int32, error) {
	for _, c := range m.GetClients() {
		if c.wallet.config.SeedRPCServerAddr.Len() > 0 {
			res, err := GetHeightContext(ctx, c.wallet.config)
			if err == nil {
				return res, err
			}
		}
	}
	return GetHeightContext(ctx, m.config)
}

// Balance wraps BalanceContext with background context.
func (m *MultiClient) Balance() (*Amount, error) {
	return m.BalanceContext(context.Background())
}

// BalanceContext is the same as package level GetBalanceContext, but using
// connected node as the RPC server, followed by this multiclient's
// SeedRPCServerAddr if failed.
func (m *MultiClient) BalanceContext(ctx context.Context) (*Amount, error) {
	return m.BalanceByAddressContext(ctx, m.GetDefaultClient().wallet.Address())
}

// BalanceByAddress wraps BalanceByAddressContext with background context.
func (m *MultiClient) BalanceByAddress(address string) (*Amount, error) {
	return m.BalanceByAddressContext(context.Background(), address)
}

// BalanceByAddressContext is the same as package level GetBalanceContext, but
// using connected node as the RPC server, followed by this multiclient's
// SeedRPCServerAddr if failed.
func (m *MultiClient) BalanceByAddressContext(ctx context.Context, address string) (*Amount, error) {
	for _, c := range m.GetClients() {
		if c.wallet.config.SeedRPCServerAddr.Len() > 0 {
			res, err := GetBalanceContext(ctx, address, c.wallet.config)
			if err == nil {
				return res, err
			}
		}
	}
	return GetBalanceContext(ctx, address, m.config)
}

// GetSubscribers wraps GetSubscribersContext with background context.
func (m *MultiClient) GetSubscribers(topic string, offset, limit int, meta, txPool bool, subscriberHashPrefix []byte) (*Subscribers, error) {
	return m.GetSubscribersContext(context.Background(), topic, offset, limit, meta, txPool, subscriberHashPrefix)
}

// GetSubscribersContext is the same as package level GetSubscribersContext, but
// using connected node as the RPC server, followed by this multiclient's
// SeedRPCServerAddr if failed.
func (m *MultiClient) GetSubscribersContext(ctx context.Context, topic string, offset, limit int, meta, txPool bool, subscriberHashPrefix []byte) (*Subscribers, error) {
	for _, c := range m.GetClients() {
		if c.wallet.config.SeedRPCServerAddr.Len() > 0 {
			res, err := GetSubscribersContext(ctx, topic, offset, limit, meta, txPool, subscriberHashPrefix, c.wallet.config)
			if err == nil {
				return res, err
			}
		}
	}
	return GetSubscribersContext(ctx, topic, offset, limit, meta, txPool, subscriberHashPrefix, m.config)
}

// GetSubscription wraps GetSubscriptionContext with background context.
func (m *MultiClient) GetSubscription(topic string, subscriber string) (*Subscription, error) {
	return m.GetSubscriptionContext(context.Background(), topic, subscriber)
}

// GetSubscriptionContext is the same as package level GetSubscriptionContext,
// but using connected node as the RPC server, followed by this multiclient's
// SeedRPCServerAddr if failed.
func (m *MultiClient) GetSubscriptionContext(ctx context.Context, topic string, subscriber string) (*Subscription, error) {
	for _, c := range m.GetClients() {
		if c.wallet.config.SeedRPCServerAddr.Len() > 0 {
			res, err := GetSubscriptionContext(ctx, topic, subscriber, c.wallet.config)
			if err == nil {
				return res, err
			}
		}
	}
	return GetSubscriptionContext(ctx, topic, subscriber, m.config)
}

// GetSubscribersCount wraps GetSubscribersCountContext with background context.
func (m *MultiClient) GetSubscribersCount(topic string, subscriberHashPrefix []byte) (int, error) {
	return m.GetSubscribersCountContext(context.Background(), topic, subscriberHashPrefix)
}

// GetSubscribersCountContext is the same as package level
// GetSubscribersCountContext, but using connected node as the RPC server,
// followed by this multiclient's SeedRPCServerAddr if failed.
func (m *MultiClient) GetSubscribersCountContext(ctx context.Context, topic string, subscriberHashPrefix []byte) (int, error) {
	for _, c := range m.GetClients() {
		if c.wallet.config.SeedRPCServerAddr.Len() > 0 {
			res, err := GetSubscribersCountContext(ctx, topic, subscriberHashPrefix, c.wallet.config)
			if err == nil {
				return res, err
			}
		}
	}
	return GetSubscribersCountContext(ctx, topic, subscriberHashPrefix, m.config)
}

// GetRegistrant wraps GetRegistrantContext with background context.
func (m *MultiClient) GetRegistrant(name string) (*Registrant, error) {
	return m.GetRegistrantContext(context.Background(), name)
}

// GetRegistrantContext is the same as package level GetRegistrantContext, but
// using connected node as the RPC server, followed by this multiclient's
// SeedRPCServerAddr if failed.
func (m *MultiClient) GetRegistrantContext(ctx context.Context, name string) (*Registrant, error) {
	for _, c := range m.GetClients() {
		if c.wallet.config.SeedRPCServerAddr.Len() > 0 {
			res, err := GetRegistrantContext(ctx, name, c.wallet.config)
			if err == nil {
				return res, err
			}
		}
	}
	return GetRegistrantContext(ctx, name, m.config)
}

// SendRawTransaction wraps SendRawTransactionContext with background context.
func (m *MultiClient) SendRawTransaction(txn *transaction.Transaction) (string, error) {
	return m.SendRawTransactionContext(context.Background(), txn)
}

// SendRawTransactionContext is the same as package level
// SendRawTransactionContext, but using connected node as the RPC server,
// followed by this multiclient's SeedRPCServerAddr if failed.
func (m *MultiClient) SendRawTransactionContext(ctx context.Context, txn *transaction.Transaction) (string, error) {
	for _, c := range m.GetClients() {
		if c.wallet.config.SeedRPCServerAddr.Len() > 0 {
			res, err := SendRawTransactionContext(ctx, txn, c.wallet.config)
			if err == nil {
				return res, err
			}
		}
	}
	return SendRawTransactionContext(ctx, txn, m.config)
}

// Transfer wraps TransferContext with background context.
func (m *MultiClient) Transfer(address, amount string, config *TransactionConfig) (string, error) {
	return m.TransferContext(context.Background(), address, amount, config)
}

// TransferContext is a shortcut for TransferContext using this multiclient as
// SignerRPCClient.
func (m *MultiClient) TransferContext(ctx context.Context, address, amount string, config *TransactionConfig) (string, error) {
	return TransferContext(ctx, m, address, amount, config)
}

// RegisterName wraps RegisterNameContext with background context.
func (m *MultiClient) RegisterName(name string, config *TransactionConfig) (string, error) {
	return m.RegisterNameContext(context.Background(), name, config)
}

// RegisterNameContext is a shortcut for RegisterNameContext using this
// multiclient as SignerRPCClient.
func (m *MultiClient) RegisterNameContext(ctx context.Context, name string, config *TransactionConfig) (string, error) {
	return RegisterNameContext(ctx, m, name, config)
}

// TransferName wraps TransferNameContext with background context.
func (m *MultiClient) TransferName(name string, recipientPubKey []byte, config *TransactionConfig) (string, error) {
	return m.TransferNameContext(context.Background(), name, recipientPubKey, config)
}

// TransferNameContext is a shortcut for TransferNameContext using this
// multiclient as SignerRPCClient.
func (m *MultiClient) TransferNameContext(ctx context.Context, name string, recipientPubKey []byte, config *TransactionConfig) (string, error) {
	return TransferNameContext(ctx, m, name, recipientPubKey, config)
}

// DeleteName wraps DeleteNameContext with background context.
func (m *MultiClient) DeleteName(name string, config *TransactionConfig) (string, error) {
	return m.DeleteNameContext(context.Background(), name, config)
}

// DeleteNameContext is a shortcut for DeleteNameContext using this multiclient
// as SignerRPCClient.
func (m *MultiClient) DeleteNameContext(ctx context.Context, name string, config *TransactionConfig) (string, error) {
	return DeleteNameContext(ctx, m, name, config)
}

// Subscribe wraps SubscribeContext with background context.
func (m *MultiClient) Subscribe(identifier, topic string, duration int, meta string, config *TransactionConfig) (string, error) {
	return m.SubscribeContext(context.Background(), identifier, topic, duration, meta, config)
}

// SubscribeContext is a shortcut for SubscribeContext using this multiclient as
// SignerRPCClient.
//
// Duration is changed to signed int for gomobile compatibility.
func (m *MultiClient) SubscribeContext(ctx context.Context, identifier, topic string, duration int, meta string, config *TransactionConfig) (string, error) {
	return SubscribeContext(ctx, m, identifier, topic, duration, meta, config)
}

// Unsubscribe wraps UnsubscribeContext with background context.
func (m *MultiClient) Unsubscribe(identifier, topic string, config *TransactionConfig) (string, error) {
	return m.UnsubscribeContext(context.Background(), identifier, topic, config)
}

// UnsubscribeContext is a shortcut for UnsubscribeContext using this
// multiclient as SignerRPCClient.
func (m *MultiClient) UnsubscribeContext(ctx context.Context, identifier, topic string, config *TransactionConfig) (string, error) {
	return UnsubscribeContext(ctx, m, identifier, topic, config)
}
