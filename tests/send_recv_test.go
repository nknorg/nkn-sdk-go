package tests

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// go test -v -timeout=0 -run=TestClientSendAndRecv
func TestClientSendAndRecv(t *testing.T) {
	listener, err := CreateClient(listenerId, seedHex)
	require.Nil(t, err)

	ch := make(chan int, 2)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		recv(listener, listenerId, false)
		close(ch)
		wg.Done()
	}()

	dialer, err := CreateClient(dialerId, seedHex)
	require.Nil(t, err)
	wg.Add(1)
	go func() {
		send(dialer, listenerAddr, dialerId, ch)
		wg.Done()
	}()

	wg.Wait()
}

// go test -v -timeout=0 -run=TestMcSendAndRecv
func TestMcSendAndRecv(t *testing.T) {
	listener, err := CreateMultiClient(listenerId, "", numClients, 100)
	require.Nil(t, err)

	ch := make(chan int, 2)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		mcrecv(listener, listenerId)
		close(ch)
		wg.Done()
	}()

	dialer, err := CreateMultiClient(dialerId, "", numClients, 100)
	require.Nil(t, err)
	wg.Add(1)
	go func() {
		mcsend(dialer, listener.Address(), dialerId, ch)
		wg.Done()
	}()

	wg.Wait()
}
