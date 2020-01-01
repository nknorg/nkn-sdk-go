package nkn_sdk_go

import (
	"encoding/hex"
	"errors"
	"log"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nknorg/nkn/util/address"
	"github.com/nknorg/nkn/vault"

	"github.com/patrickmn/go-cache"

	"github.com/nknorg/nkn-sdk-go/payloads"
)

const identifierRe = "^__\\d+__$"

type MultiClient struct {
	offset        int
	Clients       map[int]*Client
	DefaultClient *Client
	Address       string
	OnConnect     chan struct{}
	OnMessage     chan *Message
}

func getIdentifier(base string, id int) string {
	if id == -1 {
		return base
	}
	if len(base) == 0 {
		return "__" + strconv.Itoa(id) + "__"
	} else {
		return "__" + strconv.Itoa(id) + "__." + base
	}
}

func addIdentifier(addr string, id int) string {
	if id == -1 {
		return addr
	}
	return "__" + strconv.Itoa(id) + "__." + addr
}

func removeIdentifier(src string) string {
	s := strings.Split(src, ".")
	if ok, _ := regexp.MatchString(identifierRe, s[0]); ok {
		return strings.Join(s[1:], ".")
	}
	return src
}

func processDest(dest []string, clientID int) []string {
	result := make([]string, len(dest))
	for i, addr := range dest {
		result[i] = addIdentifier(addr, clientID)
	}
	return result
}

func NewMultiClient(account *vault.Account, base string, numSubClients uint16, originalClient bool, msgCacheExpiration time.Duration, configs ...ClientConfig) (*MultiClient, error) {
	config := getConfig(configs)

	numClients := int(numSubClients)
	if originalClient {
		numClients += 1
	}

	clients := make(map[int]*Client, numClients)

	offset := 0
	if originalClient {
		client, err := NewClient(account, base, configs...)
		if err != nil {
			return nil, err
		}
		clients[-1] = client
		offset = 1
	}

	for i := 0; i < int(numSubClients); i++ {
		client, err := NewClient(account, getIdentifier(base, i), configs...)
		if err != nil {
			return nil, err
		}
		clients[i] = client
	}

	var defaultClient *Client
	if originalClient {
		defaultClient = clients[-1]
	} else {
		defaultClient = clients[0]
	}

	addr := address.MakeAddressString(account.PublicKey.EncodePoint(), base)

	onConnect := make(chan struct{}, 1)
	go func() {
		cases := make([]reflect.SelectCase, len(clients))
		for i, client := range clients {
			cases[i+offset] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(client.OnConnect)}
		}
		if _, _, ok := reflect.Select(cases); ok {
			onConnect <- struct{}{}
		}
	}()

	onMessage := make(chan *Message, config.MsgChanLen)

	m := &MultiClient{
		offset:        offset,
		Clients:       clients,
		DefaultClient: defaultClient,
		Address:       addr,
		OnConnect:     onConnect,
		OnMessage:     onMessage,
	}

	c := cache.New(msgCacheExpiration, msgCacheExpiration)
	go func() {
		cases := make([]reflect.SelectCase, len(clients))
		for i, client := range clients {
			cases[i+offset] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(client.OnMessage)}
		}
		for {
			if _, value, ok := reflect.Select(cases); ok {
				msg := value.Interface().(*Message)
				cacheKey := string(msg.Pid)
				if _, ok := c.Get(cacheKey); ok {
					continue
				}
				c.Set(cacheKey, struct{}{}, cache.DefaultExpiration)

				msg.Src = removeIdentifier(msg.Src)
				msg.Reply = func(response []byte) {
					pid := msg.Pid
					var payload *payloads.Payload
					var err error
					if response == nil {
						payload, err = newAckPayload(pid)
					} else {
						payload, err = newBinaryPayload(response, pid)
					}
					if err != nil {
						log.Println("Problem creating response to PID " + hex.EncodeToString(pid))
					}
					if err := m.send([]string{msg.Src}, payload, msg.Encrypted); err != nil {
						log.Println("Problem sending response to PID " + hex.EncodeToString(pid))
					}
				}
				onMessage <- msg
			}
		}
	}()

	return m, nil
}

func (m *MultiClient) SendWithClient(clientID int, dests []string, data []byte, encrypted bool, MaxHoldingSeconds ...uint32) (*Message, error) {
	payload, err := newBinaryPayload(data, nil)
	if err != nil {
		return nil, err
	}
	pidString := string(payload.Pid)
	responseChannel := make(chan *Message, 1)
	c := m.Clients[clientID]
	c.responseChannels[pidString] = responseChannel
	if err := m.sendWithClient(clientID, dests, payload, encrypted, MaxHoldingSeconds...); err != nil {
		return nil, err
	}
	msg := <-responseChannel
	msg.Src = removeIdentifier(msg.Src)
	return msg, nil
}

func (m *MultiClient) sendWithClient(clientID int, dests []string, payload *payloads.Payload, encrypted bool, MaxHoldingSeconds ...uint32) error {
	c := m.Clients[clientID]
	return c.send(processDest(dests, clientID), payload, encrypted, MaxHoldingSeconds...)
}

func (m *MultiClient) Send(dests []string, data []byte, encrypted bool, MaxHoldingSeconds ...uint32) (*Message, error) {
	payload, err := newBinaryPayload(data, nil)
	if err != nil {
		return nil, err
	}
	responseChannels := make([]chan *Message, len(m.Clients))
	pidString := string(payload.Pid)
	offset := m.offset
	for clientID, c := range m.Clients {
		responseChannel := make(chan *Message, 1)
		responseChannels[clientID+offset] = responseChannel
		c.responseChannels[pidString] = responseChannel
		if err := m.sendWithClient(clientID, dests, payload, encrypted, MaxHoldingSeconds...); err != nil {
			return nil, err
		}
	}
	cases := make([]reflect.SelectCase, len(responseChannels))
	for i, responseChannel := range responseChannels {
		cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(responseChannel)}
	}
	if _, value, ok := reflect.Select(cases); ok {
		msg := value.Interface().(*Message)
		msg.Src = removeIdentifier(msg.Src)
		return msg, nil
	}
	return nil, errors.New("error reading response channel")
}

func (m *MultiClient) send(dests []string, payload *payloads.Payload, encrypted bool, MaxHoldingSeconds ...uint32) error {
	for clientID := range m.Clients {
		if err := m.sendWithClient(clientID, dests, payload, encrypted, MaxHoldingSeconds...); err != nil {
			return err
		}
	}
	return nil
}

func (m *MultiClient) Close() {
	for _, client := range m.Clients {
		client.Close()
	}
}
