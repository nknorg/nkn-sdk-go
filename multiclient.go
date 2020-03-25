package nkn

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	ncp "github.com/nknorg/ncp-go"
	"github.com/nknorg/nkn-sdk-go/payloads"
	"github.com/nknorg/nkn/util/address"
	"github.com/patrickmn/go-cache"
)

const (
	MultiClientIdentifierRe = "^__\\d+__$"
	DefaultSessionAllowAddr = ".*"
	SessionIDSize           = MessageIDSize
	acceptSessionBufSize    = 128
)

var (
	multiClientIdentifierRe = regexp.MustCompile(MultiClientIdentifierRe)
	ErrClosed               = ncp.NewGenericError("use of closed network connection", true, true) // the error message is meant to be identical to error returned by net package
	errAddrNotAllowed       = errors.New("address not allowed")
)

type MultiClient struct {
	config        *ClientConfig
	offset        int
	Clients       map[int]*Client
	DefaultClient *Client
	addr          *ClientAddr
	OnConnect     *OnConnect
	OnMessage     *OnMessage
	acceptSession chan *ncp.Session
	onClose       chan struct{}

	sync.RWMutex
	acceptAddrs []*regexp.Regexp
	sessions    map[string]*ncp.Session
	isClosed    bool
}

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

	clients := make(map[int]*Client, numClients)

	var wg sync.WaitGroup
	var lock sync.Mutex
	success := false
	for i := -offset; i < numSubClients; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			client, err := NewClient(account, addIdentifier(baseIdentifier, i), config)
			if err != nil {
				log.Println(err)
				return
			}
			lock.Lock()
			clients[i] = client
			success = true
			lock.Unlock()
		}(i)
	}
	wg.Wait()
	if !success {
		return nil, errors.New("failed to create any client")
	}

	var defaultClient *Client
	if originalClient {
		defaultClient = clients[-1]
	} else {
		defaultClient = clients[0]
	}

	addr := address.MakeAddressString(account.PublicKey.EncodePoint(), baseIdentifier)

	onConnect := NewOnConnect(1, nil)
	go func() {
		cases := make([]reflect.SelectCase, numClients)
		for i := 0; i < numClients; i++ {
			if clients[i-offset] != nil {
				cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(clients[i-offset].OnConnect.C)}
			} else {
				cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv}
			}
		}
		if _, value, ok := reflect.Select(cases); ok {
			nodeInfo := value.Interface().(*NodeInfo)
			onConnect.receive(nodeInfo)
		}
	}()

	m := &MultiClient{
		config:        config,
		offset:        offset,
		Clients:       clients,
		DefaultClient: defaultClient,
		addr:          NewClientAddr(addr),
		OnConnect:     onConnect,
		OnMessage:     NewOnMessage(int(config.MsgChanLen), nil),
		acceptSession: make(chan *ncp.Session, acceptSessionBufSize),
		sessions:      make(map[string]*ncp.Session, 0),
		onClose:       make(chan struct{}, 0),
	}

	msgCache := cache.New(time.Duration(config.MsgCacheExpiration)*time.Millisecond, time.Duration(config.MsgCacheExpiration)*time.Millisecond)

	go func() {
		cases := make([]reflect.SelectCase, numClients)
		for i := 0; i < numClients; i++ {
			if clients[i-offset] != nil {
				cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(clients[i-offset].OnMessage.C)}
			} else {
				cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv}
			}
		}
		for {
			select {
			case _, ok := <-m.onClose:
				if !ok {
					return
				}
			default:
			}
			if i, value, ok := reflect.Select(cases); ok {
				msg := value.Interface().(*Message)
				if msg.Type == SessionType {
					if !msg.Encrypted {
						continue
					}
					err := m.handleSessionMsg(addIdentifier("", i-offset), msg.Src, msg.MessageId, msg.Data)
					if err != nil {
						if err != ncp.ErrSessionClosed && err != errAddrNotAllowed {
							log.Println(err)
						}
						continue
					}
				} else {
					cacheKey := string(msg.MessageId)
					if _, ok := msgCache.Get(cacheKey); ok {
						continue
					}
					msgCache.Set(cacheKey, struct{}{}, cache.DefaultExpiration)

					msg.Src, _ = removeIdentifier(msg.Src)
					if msg.NoReply {
						msg.reply = func(data interface{}) error {
							return nil
						}
					} else {
						msg.reply = func(data interface{}) error {
							payload, err := newReplyPayload(data, msg.MessageId)
							if err != nil {
								return err
							}
							if err := m.send([]string{msg.Src}, payload, msg.Encrypted, 0); err != nil {
								return err
							}
							return nil
						}
					}
					m.OnMessage.receive(msg, true)
				}
			}
		}
	}()

	return m, nil
}

func (m *MultiClient) SendWithClient(clientID int, dests *StringArray, data interface{}, config *MessageConfig) (*OnMessage, error) {
	client, ok := m.Clients[clientID]
	if !ok {
		return nil, fmt.Errorf("clientID %d not found", clientID)
	}

	config, err := MergeMessageConfig(m.config.MessageConfig, config)
	if err != nil {
		return nil, err
	}

	payload, err := newMessagePayload(data, config.MessageID, config.NoReply)
	if err != nil {
		return nil, err
	}

	if err := m.sendWithClient(clientID, dests.Elems, payload, !config.Unencrypted, config.MaxHoldingSeconds); err != nil {
		return nil, err
	}

	var onReply *OnMessage
	if !config.NoReply {
		onReply = NewOnMessage(1, nil)
		client.responseChannels.Add(string(payload.MessageId), onReply, cache.DefaultExpiration)
	}

	return onReply, nil
}

// SendBinaryWithClient is a wrapper of SendWithClient for gomobile compatibility
func (m *MultiClient) SendBinaryWithClient(clientID int, dests *StringArray, data []byte, config *MessageConfig) (*OnMessage, error) {
	return m.SendWithClient(clientID, dests, data, config)
}

// SendTextWithClient is a wrapper of SendWithClient for gomobile compatibility
func (m *MultiClient) SendTextWithClient(clientID int, dests *StringArray, data string, config *MessageConfig) (*OnMessage, error) {
	return m.SendWithClient(clientID, dests, data, config)
}

func addMultiClientPrefix(dest []string, clientID int) []string {
	result := make([]string, len(dest))
	for i, addr := range dest {
		result[i] = addIdentifier(addr, clientID)
	}
	return result
}

func (m *MultiClient) sendWithClient(clientID int, dests []string, payload *payloads.Payload, encrypted bool, maxHoldingSeconds int32) error {
	client, ok := m.Clients[clientID]
	if !ok {
		return fmt.Errorf("clientID %d not found", clientID)
	}
	return client.send(addMultiClientPrefix(dests, clientID), payload, encrypted, maxHoldingSeconds)
}

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
	var errMsg []string
	var onReply, onRawReply *OnMessage

	success := make(chan struct{}, 0)
	fail := make(chan struct{}, 0)

	if !config.NoReply {
		onReply = NewOnMessage(1, nil)
		onRawReply = NewOnMessage(1, nil)

		// response channel is added first to prevent some client fail to handle response if send finish before receive response
		for _, client := range m.Clients {
			client.responseChannels.Add(string(payload.MessageId), onRawReply, cache.DefaultExpiration)
		}
	}

	go func() {
		sent := 0
		for clientID := range m.Clients {
			if err := m.sendWithClient(clientID, dests.Elems, payload, !config.Unencrypted, config.MaxHoldingSeconds); err == nil {
				select {
				case success <- struct{}{}:
				default:
				}
				sent++
			} else {
				lock.Lock()
				errMsg = append(errMsg, err.Error())
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
		return nil, errors.New(strings.Join(errMsg, "; "))
	}
}

// SendBinary is a wrapper of Send for gomobile compatibility
func (m *MultiClient) SendBinary(dests *StringArray, data []byte, config *MessageConfig) (*OnMessage, error) {
	return m.Send(dests, data, config)
}

// SendText is a wrapper of Send for gomobile compatibility
func (m *MultiClient) SendText(dests *StringArray, data string, config *MessageConfig) (*OnMessage, error) {
	return m.Send(dests, data, config)
}

func (m *MultiClient) send(dests []string, payload *payloads.Payload, encrypted bool, maxHoldingSeconds int32) error {
	var lock sync.Mutex
	var errMsg []string
	success := make(chan struct{}, 0)
	fail := make(chan struct{}, 0)
	go func() {
		sent := 0
		for clientID := range m.Clients {
			if err := m.sendWithClient(clientID, dests, payload, encrypted, maxHoldingSeconds); err == nil {
				select {
				case success <- struct{}{}:
				default:
				}
				sent++
			} else {
				lock.Lock()
				errMsg = append(errMsg, err.Error())
				lock.Unlock()
			}
		}
		if sent == 0 {
			fail <- struct{}{}
		}
	}()

	select {
	case <-success:
		return nil
	case <-fail:
		return errors.New(strings.Join(errMsg, "; "))
	}
}

func (m *MultiClient) Publish(topic string, data interface{}, config *MessageConfig) error {
	return publish(m, topic, data, config)
}

// PublishBinary is a wrapper of Publish for gomobile compatibility
func (m *MultiClient) PublishBinary(topic string, data []byte, config *MessageConfig) error {
	return m.Publish(topic, data, config)
}

// PublishText is a wrapper of Publish for gomobile compatibility
func (m *MultiClient) PublishText(topic string, data string, config *MessageConfig) error {
	return m.Publish(topic, data, config)
}

func (m *MultiClient) newSession(remoteAddr string, sessionID []byte, config *ncp.Config) (*ncp.Session, error) {
	clientIDs := make([]string, 0, len(m.Clients))
	clients := make(map[string]*Client, len(m.Clients))
	for id, client := range m.Clients {
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
		if writeTimeout > 0 {
			err := client.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err != nil {
				return err
			}
		}
		err := client.send([]string{addIdentifierPrefix(remoteAddr, remoteClientID)}, payload, true, 0)
		if err != nil {
			return err
		}
		if writeTimeout > 0 {
			err = client.SetWriteDeadline(zeroTime)
			if err != nil {
				return err
			}
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

	m.Lock()
	session, ok := m.sessions[sessionKey]
	if !ok {
		if !m.shouldAcceptAddr(remoteAddr) {
			return errAddrNotAllowed
		}

		session, err = m.newSession(remoteAddr, sessionID, m.config.SessionConfig)
		if err != nil {
			m.Unlock()
			return err
		}
		m.sessions[sessionKey] = session
	}
	m.Unlock()

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

func (m *MultiClient) Address() string {
	return m.addr.String()
}

func (m *MultiClient) Addr() net.Addr {
	return m.addr
}

func (m *MultiClient) Listen(addrsRe *StringArray) error {
	var addrs []string
	if addrsRe == nil {
		addrs = []string{DefaultSessionAllowAddr}
	} else {
		addrs = addrsRe.Elems
	}

	var err error
	acceptAddrs := make([]*regexp.Regexp, len(addrs))
	for i := 0; i < len(acceptAddrs); i++ {
		acceptAddrs[i], err = regexp.Compile(addrs[i])
		if err != nil {
			return err
		}
	}

	m.Lock()
	m.acceptAddrs = acceptAddrs
	m.Unlock()

	return nil
}

func (m *MultiClient) Dial(remoteAddr string) (net.Conn, error) {
	return m.DialSession(remoteAddr)
}

func (m *MultiClient) DialSession(remoteAddr string) (*ncp.Session, error) {
	return m.DialWithConfig(remoteAddr, nil)
}

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

	m.Lock()
	m.sessions[sessionKey] = session
	m.Unlock()

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
		case _, ok := <-m.onClose:
			if !ok {
				return nil, ErrClosed
			}
		}
	}
}

func (m *MultiClient) Accept() (net.Conn, error) {
	return m.AcceptSession()
}

func (m *MultiClient) Close() error {
	m.Lock()
	defer m.Unlock()

	if m.isClosed {
		return nil
	}

	for _, session := range m.sessions {
		err := session.Close()
		if err != nil {
			log.Println(err)
			continue
		}
	}

	time.AfterFunc(time.Duration(m.config.SessionConfig.Linger)*time.Millisecond, func() {
		for _, client := range m.Clients {
			client.Close()
		}
	})

	m.isClosed = true

	close(m.onClose)

	return nil
}

func (m *MultiClient) IsClosed() bool {
	m.RLock()
	defer m.RUnlock()
	return m.isClosed
}

func (m *MultiClient) getConfig() *ClientConfig {
	return m.config
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
