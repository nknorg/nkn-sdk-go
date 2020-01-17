package nkn_sdk_go

import (
	"errors"
	"fmt"
	"log"
	"net"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/nknorg/nkn/util/address"
	"github.com/nknorg/nkn/vault"

	"github.com/patrickmn/go-cache"

	"github.com/nknorg/ncp"
	"github.com/nknorg/nkn-sdk-go/payloads"
)

const (
	identifierRe  = "^__\\d+__$"
	SessionIDSize = 8 // in bytes
)

type MultiClient struct {
	config        *ClientConfig
	offset        int
	Clients       map[int]*Client
	DefaultClient *Client
	addr          Addr
	Address       string
	OnConnect     chan struct{}
	OnMessage     chan *Message
	acceptSession chan *ncp.Session
	onClose       chan struct{}

	sync.RWMutex
	sessions map[string]*ncp.Session
	isClosed bool
}

func NewMultiClient(account *vault.Account, baseIdentifier string, numSubClients int, originalClient bool, configs ...ClientConfig) (*MultiClient, error) {
	config, err := MergeClientConfig(configs)
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
			client, err := NewClient(account, addIdentifier(baseIdentifier, i), configs...)
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

	onConnect := make(chan struct{}, 1)
	go func() {
		cases := make([]reflect.SelectCase, numClients)
		for i := 0; i < numClients; i++ {
			if clients[i-offset] != nil {
				cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(clients[i-offset].OnConnect)}
			} else {
				cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv}
			}
		}
		if _, _, ok := reflect.Select(cases); ok {
			onConnect <- struct{}{}
		}
	}()

	onMessage := make(chan *Message, config.MsgChanLen)

	m := &MultiClient{
		config:        config,
		offset:        offset,
		Clients:       clients,
		DefaultClient: defaultClient,
		addr:          Addr{addr: addr},
		Address:       addr,
		OnConnect:     onConnect,
		OnMessage:     onMessage,
		acceptSession: make(chan *ncp.Session, 128),
		sessions:      make(map[string]*ncp.Session, 0),
		onClose:       make(chan struct{}, 0),
	}

	msgCache := cache.New(config.MsgCacheExpiration, config.MsgCacheExpiration)

	go func() {
		cases := make([]reflect.SelectCase, numClients)
		for i := 0; i < numClients; i++ {
			if clients[i-offset] != nil {
				cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(clients[i-offset].OnMessage)}
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
				if msg.IsSession {
					if !msg.Encrypted {
						continue
					}
					err := m.handleSessionMsg(addIdentifier("", i-offset), msg.Src, msg.Pid, msg.Data)
					if err != nil {
						if err != ncp.SessionClosed {
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
					msg.Reply = func(response []byte) error {
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
					onMessage <- msg
				}
			}
		}
	}()

	return m, nil
}

func (m *MultiClient) SendWithClient(clientID int, dests []string, data []byte, configs ...*MessageConfig) (chan *Message, error) {
	client, ok := m.Clients[clientID]
	if !ok {
		return nil, fmt.Errorf("clientID %d not found", clientID)
	}

	config, err := MergeMessageConfig(&m.config.MessageConfig, configs)
	if err != nil {
		return nil, err
	}

	payload, err := newBinaryPayload(data, nil, config.NoAck)
	if err != nil {
		return nil, err
	}

	if err := m.sendWithClient(clientID, dests, payload, !config.Unencrypted, config.MaxHoldingSeconds); err != nil {
		return nil, err
	}

	pidString := string(payload.Pid)
	respChan := make(chan *Message, 1)
	client.responseChannels.Add(pidString, respChan, cache.DefaultExpiration)

	return respChan, nil
}

func (m *MultiClient) sendWithClient(clientID int, dests []string, payload *payloads.Payload, encrypted bool, maxHoldingSeconds int32) error {
	client, ok := m.Clients[clientID]
	if !ok {
		return fmt.Errorf("clientID %d not found", clientID)
	}
	return client.send(processDest(dests, clientID), payload, encrypted, maxHoldingSeconds)
}

func (m *MultiClient) Send(dests []string, data []byte, configs ...*MessageConfig) (chan *Message, error) {
	config, err := MergeMessageConfig(&m.config.MessageConfig, configs)
	if err != nil {
		return nil, err
	}

	payload, err := newBinaryPayload(data, nil, config.NoAck)
	if err != nil {
		return nil, err
	}

	respChan := make(chan *Message, 1)
	responseChannels := make([]chan *Message, len(m.Clients))
	pidString := string(payload.Pid)
	offset := m.offset
	success := false
	for clientID, client := range m.Clients {
		if err := m.sendWithClient(clientID, dests, payload, !config.Unencrypted, config.MaxHoldingSeconds); err == nil {
			success = true
			ch := make(chan *Message, 1)
			responseChannels[clientID+offset] = ch
			client.responseChannels.Add(pidString, ch, cache.DefaultExpiration)
		}
	}
	if !success {
		return nil, errors.New("all clients failed to send msg")
	}

	go func() {
		cases := make([]reflect.SelectCase, len(responseChannels))
		for i, responseChannel := range responseChannels {
			if responseChannel != nil {
				cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(responseChannel)}
			} else {
				cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv}
			}
		}
		if _, value, ok := reflect.Select(cases); ok {
			msg := value.Interface().(*Message)
			msg.Src, _ = removeIdentifier(msg.Src)
			respChan <- msg
		}
	}()

	return respChan, nil
}

func (m *MultiClient) send(dests []string, payload *payloads.Payload, encrypted bool, maxHoldingSeconds int32) error {
	success := false
	for clientID := range m.Clients {
		if err := m.sendWithClient(clientID, dests, payload, encrypted, maxHoldingSeconds); err == nil {
			success = true
		}
	}
	if !success {
		return errors.New("all clients failed to send msg")
	}
	return nil
}

func (m *MultiClient) Publish(topic string, data []byte, configs ...*MessageConfig) error {
	return publish(m, topic, data, configs...)
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
	return ncp.NewSession(m.addr, Addr{addr: remoteAddr}, clientIDs, nil, (func(localClientID, remoteClientID string, buf []byte, writeTimeout time.Duration) error {
		payload := &payloads.Payload{
			Type:      payloads.BINARY,
			Pid:       sessionID,
			Data:      buf,
			IsSession: true,
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

	m.Lock()
	session, ok := m.sessions[sessionKey]
	if !ok {
		session, err := m.newSession(remoteAddr, sessionID, &m.config.SessionConfig)
		if err != nil {
			m.Unlock()
			return err
		}

		m.sessions[sessionKey] = session
		m.Unlock()

		err = session.ReceiveWith(localClientID, remoteClientID, data)
		if err != nil {
			return err
		}

		select {
		case m.acceptSession <- session:
		default:
			log.Println("Accept session channel full, discard request...")
		}
	} else {
		m.Unlock()
		err := session.ReceiveWith(localClientID, remoteClientID, data)
		if err != nil {
			return err
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
	config, err = MergeSessionConfig(&m.config.SessionConfig, config)
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
				return nil, ncp.Closed
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

	for _, client := range m.Clients {
		client.Close()
	}

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
