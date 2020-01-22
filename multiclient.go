package nkn

import (
	"errors"
	"fmt"
	"log"
	"net"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/nknorg/ncp"
	"github.com/nknorg/nkn-sdk-go/payloads"
	"github.com/nknorg/nkn/util/address"
	"github.com/patrickmn/go-cache"
)

const (
	identifierRe         = "^__\\d+__$"
	SessionIDSize        = 8 // in bytes
	acceptSessionBufSize = 128
)

var (
	ErrClosed = ncp.NewGenericError("use of closed network connection", true, true) // the error message is meant to be identical to error returned by net package
)

type MultiClient struct {
	config        *ClientConfig
	offset        int
	Clients       map[int]*Client
	DefaultClient *Client
	addr          *nknAddr
	Address       string
	OnConnect     *OnConnect
	OnMessage     *OnMessage
	acceptSession chan *ncp.Session
	onClose       chan struct{}

	sync.RWMutex
	sessions map[string]*ncp.Session
	isClosed bool
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
		addr:          &nknAddr{addr: addr},
		Address:       addr,
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
					err := m.handleSessionMsg(addIdentifier("", i-offset), msg.Src, msg.Pid, msg.Data)
					if err != nil {
						if err != ncp.ErrSessionClosed {
							log.Println(err)
						}
						continue
					}
				} else {
					cacheKey := string(msg.Pid)
					if _, ok := msgCache.Get(cacheKey); ok {
						continue
					}
					msgCache.Set(cacheKey, struct{}{}, cache.DefaultExpiration)

					msg.Src, _ = removeIdentifier(msg.Src)
					msg.reply = func(response []byte) error {
						pid := msg.Pid
						var payload *payloads.Payload
						var err error
						if response == nil {
							payload, err = newAckPayload(pid)
						} else {
							payload, err = newBinaryPayload(response, pid, false)
						}
						if err != nil {
							return err
						}
						if err := m.send([]string{msg.Src}, payload, msg.Encrypted, 0); err != nil {
							return err
						}
						return nil
					}
					m.OnMessage.receive(msg, true)
				}
			}
		}
	}()

	return m, nil
}

func (m *MultiClient) SendWithClient(clientID int, dests *StringArray, data []byte, config *MessageConfig) (*OnMessage, error) {
	client, ok := m.Clients[clientID]
	if !ok {
		return nil, fmt.Errorf("clientID %d not found", clientID)
	}

	config, err := MergeMessageConfig(m.config.MessageConfig, config)
	if err != nil {
		return nil, err
	}

	payload, err := newBinaryPayload(data, nil, config.NoAck)
	if err != nil {
		return nil, err
	}

	if err := m.sendWithClient(clientID, dests.Elems, payload, !config.Unencrypted, config.MaxHoldingSeconds); err != nil {
		return nil, err
	}

	pidString := string(payload.Pid)
	onReply := NewOnMessage(1, nil)
	client.responseChannels.Add(pidString, onReply, cache.DefaultExpiration)

	return onReply, nil
}

func (m *MultiClient) sendWithClient(clientID int, dests []string, payload *payloads.Payload, encrypted bool, maxHoldingSeconds int32) error {
	client, ok := m.Clients[clientID]
	if !ok {
		return fmt.Errorf("clientID %d not found", clientID)
	}
	return client.send(processDest(dests, clientID), payload, encrypted, maxHoldingSeconds)
}

func (m *MultiClient) Send(dests *StringArray, data []byte, config *MessageConfig) (*OnMessage, error) {
	config, err := MergeMessageConfig(m.config.MessageConfig, config)
	if err != nil {
		return nil, err
	}

	payload, err := newBinaryPayload(data, nil, config.NoAck)
	if err != nil {
		return nil, err
	}

	onReply := NewOnMessage(1, nil)
	onRawReply := NewOnMessage(0, nil)
	success := make(chan struct{}, 0)
	fail := make(chan struct{}, 0)

	go func() {
		sent := 0
		for clientID, client := range m.Clients {
			if err := m.sendWithClient(clientID, dests.Elems, payload, !config.Unencrypted, config.MaxHoldingSeconds); err == nil {
				select {
				case success <- struct{}{}:
				default:
				}
				sent++
				client.responseChannels.Add(string(payload.Pid), onRawReply, cache.DefaultExpiration)
			}
		}
		if sent == 0 {
			select {
			case fail <- struct{}{}:
			default:
			}
		}

		msg := <-onRawReply.C
		msg.Src, _ = removeIdentifier(msg.Src)
		onReply.receive(msg, false)
	}()

	select {
	case <-success:
		return onReply, nil
	case <-fail:
		return nil, errors.New("all clients failed to send msg")
	}
}

func (m *MultiClient) send(dests []string, payload *payloads.Payload, encrypted bool, maxHoldingSeconds int32) error {
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
		return errors.New("all clients failed to send msg")
	}
}

func (m *MultiClient) Publish(topic string, data []byte, config *MessageConfig) error {
	return publish(m, topic, data, config)
}

func (m *MultiClient) newSession(remoteAddr string, sessionID []byte, config *SessionConfig) (*ncp.Session, error) {
	clientIDs := make([]string, 0, len(m.Clients))
	clients := make(map[string]*Client, len(m.Clients))
	for id, client := range m.Clients {
		clientID := addIdentifier("", id)
		clientIDs = append(clientIDs, clientID)
		clients[clientID] = client
	}
	sort.Strings(clientIDs)
	return ncp.NewSession(m.addr, &nknAddr{addr: remoteAddr}, clientIDs, nil, (func(localClientID, remoteClientID string, buf []byte, writeTimeout time.Duration) error {
		payload := &payloads.Payload{
			Type: payloads.SESSION,
			Pid:  sessionID,
			Data: buf,
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
	}), (*ncp.Config)(config))
}

func (m *MultiClient) handleSessionMsg(localClientID, src string, sessionID, data []byte) error {
	remoteAddr, remoteClientID := removeIdentifier(src)
	sessionKey := sessionKey(remoteAddr, sessionID)
	var err error

	m.Lock()
	session, ok := m.sessions[sessionKey]
	if !ok {
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

func (m *MultiClient) Addr() net.Addr {
	return m.addr
}

func (m *MultiClient) Dial(remoteAddr string) (*ncp.Session, error) {
	return m.DialWithConfig(remoteAddr, nil)
}

func (m *MultiClient) DialWithConfig(remoteAddr string, config *SessionConfig) (*ncp.Session, error) {
	var err error
	config, err = MergeSessionConfig(m.config.SessionConfig, config)
	if err != nil {
		return nil, err
	}

	sessionID, err := RandomBytes(SessionIDSize)
	if err != nil {
		return nil, err
	}
	sessionKey := sessionKey(remoteAddr, sessionID)
	session, err := m.newSession(remoteAddr, sessionID, config)
	if err != nil {
		return nil, err
	}

	m.Lock()
	m.sessions[sessionKey] = session
	m.Unlock()

	err = session.Dial()
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