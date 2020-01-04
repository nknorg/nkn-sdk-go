package nkn_sdk_go

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/nknorg/nkn-sdk-go/payloads"
)

const (
	MinSequenceID = 1
	SessionIDSize = 8 // in bytes
)

var (
	maxWait    = time.Second
	errMaxWait = errors.New("max wait time reached")
)

type Session struct {
	config           *SessionConfig
	localAddr        *Addr
	remoteAddr       *Addr
	clientIDs        []string
	sendWith         SendWithFunc
	sendWindowSize   uint32
	recvWindowSize   uint32
	sendMtu          uint32
	recvMtu          uint32
	connections      map[string]*Connection
	onAccept         chan struct{}
	sendChan         chan uint32
	resendChan       chan uint32
	sendWindowUpdate chan struct{}
	recvDataUpdate   chan struct{}
	ctx              context.Context
	cancel           context.CancelFunc
	readContext      context.Context
	readCancel       context.CancelFunc
	writeContext     context.Context
	writeCancel      context.CancelFunc
	readLock         sync.Mutex
	writeLock        sync.Mutex

	acceptLock sync.Mutex
	isAccepted bool

	sync.RWMutex
	isEstablished      bool
	isClosed           bool
	sendBuffer         []byte
	sendWindowStartSeq uint32
	sendWindowEndSeq   uint32
	sendWindowUsed     uint32
	sendWindowData     map[uint32][]byte
	sendWindowDataSize map[uint32]uint32
	recvWindowStartSeq uint32
	recvWindowUsed     uint32
	recvWindowData     map[uint32][]byte
}

type SendWithFunc func(clientID, dest string, buf []byte, writeTimeout time.Duration) error

func NewSession(localAddr, remoteAddr string, clientIDs []string, sendWith SendWithFunc, config *SessionConfig) (*Session, error) {
	config, err := MergedSessionConfig(config)
	if err != nil {
		return nil, err
	}

	session := &Session{
		config:             config,
		localAddr:          &Addr{addr: localAddr},
		remoteAddr:         &Addr{addr: remoteAddr},
		clientIDs:          clientIDs,
		sendWith:           sendWith,
		sendWindowSize:     config.SessionWindowSize,
		recvWindowSize:     config.SessionWindowSize,
		sendMtu:            config.MTU,
		recvMtu:            config.MTU,
		sendWindowStartSeq: MinSequenceID,
		sendWindowEndSeq:   MinSequenceID,
		recvWindowStartSeq: MinSequenceID,
		onAccept:           make(chan struct{}, 1),
	}

	session.ctx, session.cancel = context.WithCancel(context.Background())
	session.SetReadDeadline(zeroTime)
	session.SetWriteDeadline(zeroTime)

	return session, nil
}

func (session *Session) IsStream() bool {
	return !session.config.NonStream
}

func (session *Session) IsEstablished() bool {
	session.RLock()
	defer session.RUnlock()
	return session.isEstablished
}

func (session *Session) IsClosed() bool {
	session.RLock()
	defer session.RUnlock()
	return session.isClosed
}

func (session *Session) SendWindowUsed() uint32 {
	session.RLock()
	defer session.RUnlock()
	return session.sendWindowUsed
}

func (session *Session) RecvWindowUsed() uint32 {
	session.RLock()
	defer session.RUnlock()
	return session.recvWindowUsed
}

func (session *Session) GetDataToSend(sequenceID uint32) []byte {
	session.RLock()
	defer session.RUnlock()
	return session.sendWindowData[sequenceID]
}

func (session *Session) GetConnWindowSize() uint32 {
	session.RLock()
	defer session.RUnlock()
	var windowSize uint32
	for _, conn := range session.connections {
		windowSize += uint32(conn.windowSize)
	}
	return windowSize
}

func (session *Session) GetResendSeq() (uint32, error) {
	var seq uint32
	select {
	case seq = <-session.resendChan:
	case <-session.ctx.Done():
		return 0, session.ctx.Err()
	default:
	}
	return seq, nil
}

func (session *Session) GetSendSeq() (uint32, error) {
	var seq uint32
	select {
	case seq = <-session.resendChan:
	case seq = <-session.sendChan:
	case <-session.ctx.Done():
		return 0, session.ctx.Err()
	}
	return seq, nil
}

func (session *Session) ReceiveWith(clientID, src string, buf []byte) error {
	if session.IsClosed() {
		return SessionClosed
	}

	data := &payloads.SessionData{}
	err := proto.Unmarshal(buf, data)
	if err != nil {
		return err
	}

	if data.Close {
		return session.handleClosePacket()
	}

	isEstablished := session.IsEstablished()
	if !isEstablished && data.SequenceId == 0 && len(data.AckStartSeq) == 0 && len(data.AckSeqCount) == 0 {
		return session.handleHandshakePacket(data)
	}

	if isEstablished && (len(data.AckStartSeq) > 0 || len(data.AckSeqCount) > 0) {
		if len(data.AckStartSeq) > 0 && len(data.AckSeqCount) > 0 && len(data.AckStartSeq) != len(data.AckSeqCount) {
			return fmt.Errorf("AckStartSeq length %d is different from AckSeqCount length %d", len(data.AckStartSeq), len(data.AckSeqCount))
		}

		count := 0
		if len(data.AckStartSeq) > 0 {
			count = len(data.AckStartSeq)
		} else {
			count = len(data.AckSeqCount)
		}

		var ackStartSeq, ackEndSeq uint32
		for i := 0; i < count; i++ {
			if len(data.AckStartSeq) > 0 {
				ackStartSeq = data.AckStartSeq[i]
			} else {
				ackStartSeq = MinSequenceID
			}

			if len(data.AckSeqCount) > 0 {
				ackEndSeq = NextSeq(ackStartSeq, data.AckSeqCount[i])
			} else {
				ackEndSeq = NextSeq(ackStartSeq, 1)
			}

			session.Lock()
			if SeqInBetween(session.sendWindowStartSeq, session.sendWindowEndSeq, PrevSeq(ackEndSeq, 1)) {
				if !SeqInBetween(session.sendWindowStartSeq, session.sendWindowEndSeq, ackStartSeq) {
					ackStartSeq = session.sendWindowStartSeq
				}
				for seq := ackStartSeq; SeqInBetween(ackStartSeq, ackEndSeq, seq); seq = NextSeq(seq, 1) {
					for _, conn := range session.connections {
						conn.ReceiveACK(seq)
					}
					delete(session.sendWindowData, seq)
				}
				if ackStartSeq == session.sendWindowStartSeq {
					for {
						session.sendWindowUsed -= session.sendWindowDataSize[session.sendWindowStartSeq]
						delete(session.sendWindowDataSize, session.sendWindowStartSeq)
						session.sendWindowStartSeq = NextSeq(session.sendWindowStartSeq, 1)
						if _, ok := session.sendWindowData[session.sendWindowStartSeq]; ok {
							break
						}
						if session.sendWindowStartSeq == session.sendWindowEndSeq {
							break
						}
					}
					select {
					case session.sendWindowUpdate <- struct{}{}:
					default:
					}
				}
			}
			session.Unlock()
		}
	}

	if isEstablished && data.SequenceId > 0 {
		if uint32(len(data.Data)) > session.recvMtu {
			return errors.New("received data exceeds mtu")
		}

		session.Lock()
		if CompareSeq(data.SequenceId, session.recvWindowStartSeq) >= 0 {
			if _, ok := session.recvWindowData[data.SequenceId]; !ok {
				if session.recvWindowUsed+uint32(len(data.Data)) > session.recvWindowSize {
					session.Unlock()
					return errors.New("receive window full")
				}

				session.recvWindowData[data.SequenceId] = data.Data
				session.recvWindowUsed += uint32(len(data.Data))

				if data.SequenceId == session.recvWindowStartSeq {
					select {
					case session.recvDataUpdate <- struct{}{}:
					default:
					}
				}
			}
		}
		session.Unlock()

		if conn, ok := session.connections[connKey(clientID, src)]; ok {
			conn.SendACK(data.SequenceId)
		}
	}

	return nil
}

func (session *Session) start() error {
	for _, conn := range session.connections {
		conn.Start()
	}
	var err error
	for {
		select {
		case <-time.After(session.config.FlushInterval):
		case <-session.ctx.Done():
			return session.ctx.Err()
		}

		session.RLock()
		shouldFlush := len(session.sendBuffer) > 0
		session.RUnlock()

		if !shouldFlush {
			continue
		}

		err = session.flushSendBuffer()
		if err != nil {
			if session.ctx.Err() != nil {
				return session.ctx.Err()
			}
			log.Println(err)
			continue
		}
	}
}

func (session *Session) waitForSendWindow(ctx context.Context, n uint32) (uint32, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	for session.SendWindowUsed()+n > session.sendWindowSize {
		select {
		case <-session.sendWindowUpdate:
		case <-time.After(maxWait):
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	return session.sendWindowSize - session.SendWindowUsed(), nil
}

func (session *Session) flushSendBuffer() error {
	session.Lock()

	if len(session.sendBuffer) == 0 {
		session.Unlock()
		return nil
	}

	seq := session.sendWindowEndSeq
	buf, err := proto.Marshal(&payloads.SessionData{
		SequenceId: seq,
		Data:       session.sendBuffer[:len(session.sendBuffer)],
	})
	if err != nil {
		session.Unlock()
		return err
	}

	session.sendWindowData[seq] = buf
	session.sendWindowDataSize[seq] = uint32(len(session.sendBuffer))
	session.sendWindowEndSeq = NextSeq(seq, 1)
	session.sendBuffer = make([]byte, 0, session.sendMtu)

	session.Unlock()

	select {
	case session.sendChan <- seq:
	case <-session.ctx.Done():
		return session.ctx.Err()
	}

	return nil
}

func (session *Session) sendHandshakePacket(writeTimeout time.Duration) error {
	data := &payloads.SessionData{
		IdentifierPrefix: session.clientIDs,
		WindowSize:       session.recvWindowSize,
		Mtu:              session.recvMtu,
	}
	buf, err := proto.Marshal(data)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	success := false
	if len(session.connections) > 0 {
		for _, connection := range session.connections {
			wg.Add(1)
			go func(connection *Connection) {
				defer wg.Done()
				err := session.sendWith(connection.clientID, connection.remoteAddr, buf, writeTimeout)
				if err != nil {
					log.Println(err)
				} else {
					success = true
				}
			}(connection)
		}
	} else {
		for _, clientID := range session.clientIDs {
			wg.Add(1)
			go func(clientID string) {
				defer wg.Done()
				err := session.sendWith(clientID, addIdentifierPrefix(session.remoteAddr.addr, clientID), buf, writeTimeout)
				if err != nil {
					log.Println(err)
				} else {
					success = true
				}
			}(clientID)
		}
	}
	wg.Wait()
	if !success {
		return errors.New("send handshake packet failed")
	}

	return nil
}

func (session *Session) handleHandshakePacket(data *payloads.SessionData) error {
	session.Lock()
	defer session.Unlock()
	if !session.isEstablished {
		if data.WindowSize == 0 {
			return errors.New("empty remote window size")
		}
		if data.WindowSize < session.sendWindowSize {
			session.sendWindowSize = data.WindowSize
		}

		if data.Mtu == 0 {
			return errors.New("empty mtu")
		}
		if data.Mtu < session.sendMtu {
			session.sendMtu = data.Mtu
		}

		if len(data.IdentifierPrefix) == 0 {
			return errors.New("empty identifier prefix")
		}
		n := len(session.clientIDs)
		if len(data.IdentifierPrefix) < n {
			n = len(data.IdentifierPrefix)
		}

		connections := make(map[string]*Connection, n)
		for i := 0; i < n; i++ {
			remoteAddr := addIdentifierPrefix(session.remoteAddr.addr, data.IdentifierPrefix[i])
			conn, err := session.NewConnection(session.clientIDs[i], remoteAddr)
			if err != nil {
				return err
			}
			connections[connKey(conn.clientID, conn.remoteAddr)] = conn
		}
		session.connections = connections

		session.sendChan = make(chan uint32)
		session.resendChan = make(chan uint32, session.config.MaxConnectionWindowSize*uint32(n))
		session.sendWindowUpdate = make(chan struct{}, 1)
		session.recvDataUpdate = make(chan struct{}, 1)
		session.sendBuffer = make([]byte, 0, session.sendMtu)
		session.sendWindowData = make(map[uint32][]byte)
		session.sendWindowDataSize = make(map[uint32]uint32)
		session.recvWindowData = make(map[uint32][]byte)

		select {
		case session.onAccept <- struct{}{}:
		default:
		}

		session.isEstablished = true
	}
	return nil
}

func (session *Session) sendClosePacket() error {
	if !session.IsEstablished() {
		return SessionNotEstablished
	}

	data := &payloads.SessionData{
		Close: true,
	}
	buf, err := proto.Marshal(data)
	if err != nil {
		return err
	}

	success := false
	for _, connection := range session.connections {
		err = session.sendWith(connection.clientID, connection.remoteAddr, buf, connection.RetransmissionTimeout())
		if err != nil {
			log.Println(err)
		} else {
			success = true
		}
	}
	if !success {
		return errors.New("send close packet failed")
	}

	return nil
}

func (session *Session) handleClosePacket() error {
	go func() {
		err := session.flushSendBuffer()
		if err != nil {
			log.Println(err)
		}
	}()
	time.AfterFunc(session.config.CloseDelay, session.close)
	return nil
}

func (session *Session) Dial() error {
	var dialTimeoutChan <-chan time.Time
	if session.config.DialTimeout > 0 {
		dialTimeoutChan = time.After(session.config.DialTimeout)
	}

	session.acceptLock.Lock()
	defer session.acceptLock.Unlock()
	if session.isAccepted {
		return SessionEstablished
	}

	err := session.sendHandshakePacket(session.config.DialTimeout)
	if err != nil {
		return err
	}

	select {
	case <-session.onAccept:
	case <-dialTimeoutChan:
		return DialTimeout
	}

	go session.start()
	session.isAccepted = true
	return nil
}

func (session *Session) Accept() error {
	session.acceptLock.Lock()
	defer session.acceptLock.Unlock()
	if session.isAccepted {
		return SessionEstablished
	}

	select {
	case <-session.onAccept:
	default:
		return errors.New("receive non-handshake first packet")
	}

	go session.start()
	session.isAccepted = true
	return session.sendHandshakePacket(session.config.MaxRetransmissionTimeout)
}

func (session *Session) Read(b []byte) (_ int, e error) {
	defer func() {
		if e == context.DeadlineExceeded {
			e = ReadDeadlineExceeded
		}
		if e == context.Canceled {
			e = SessionClosed
		}
	}()

	if session.IsClosed() {
		return 0, SessionClosed
	}

	if !session.IsEstablished() {
		return 0, SessionNotEstablished
	}

	if len(b) == 0 {
		return 0, nil
	}

	session.readLock.Lock()
	defer session.readLock.Unlock()

	for {
		if err := session.readContext.Err(); err != nil {
			return 0, err
		}

		session.RLock()
		_, ok := session.recvWindowData[session.recvWindowStartSeq]
		session.RUnlock()
		if ok {
			break
		}

		select {
		case <-session.recvDataUpdate:
		case <-time.After(maxWait):
		case <-session.readContext.Done():
			return 0, session.readContext.Err()
		}
	}

	session.Lock()
	defer session.Unlock()

	data := session.recvWindowData[session.recvWindowStartSeq]
	if !session.IsStream() && len(b) < len(session.recvWindowData[session.recvWindowStartSeq]) {
		return 0, BufferSizeTooSmall
	}

	bytesReceived := copy(b, data)
	if bytesReceived == len(data) {
		delete(session.recvWindowData, session.recvWindowStartSeq)
		session.recvWindowStartSeq = NextSeq(session.recvWindowStartSeq, 1)
	} else {
		session.recvWindowData[session.recvWindowStartSeq] = data[bytesReceived:]
	}
	session.recvWindowUsed -= uint32(bytesReceived)

	if session.IsStream() {
		for bytesReceived < len(b) {
			data, ok := session.recvWindowData[session.recvWindowStartSeq]
			if !ok {
				break
			}
			n := copy(b[bytesReceived:], data)
			if n == len(data) {
				delete(session.recvWindowData, session.recvWindowStartSeq)
				session.recvWindowStartSeq = NextSeq(session.recvWindowStartSeq, 1)
			} else {
				session.recvWindowData[session.recvWindowStartSeq] = data[n:]
			}
			session.recvWindowUsed -= uint32(n)
			bytesReceived += n
		}
	}

	return bytesReceived, nil
}

func (session *Session) Write(b []byte) (_ int, e error) {
	defer func() {
		if e == context.DeadlineExceeded {
			e = WriteDeadlineExceeded
		}
		if e == context.Canceled {
			e = SessionClosed
		}
	}()

	if session.IsClosed() {
		return 0, SessionClosed
	}

	if !session.IsEstablished() {
		return 0, SessionNotEstablished
	}

	if !session.IsStream() && (len(b) > int(session.sendMtu) || len(b) > int(session.sendWindowSize)) {
		return 0, DataSizeTooLarge
	}

	if len(b) == 0 {
		return 0, nil
	}

	session.writeLock.Lock()
	defer session.writeLock.Unlock()

	bytesSent := 0
	if session.IsStream() {
		for len(b) > 0 {
			sendWindowAvailable, err := session.waitForSendWindow(session.writeContext, 1)
			if err != nil {
				return bytesSent, err
			}

			n := len(b)
			if n > int(sendWindowAvailable) {
				n = int(sendWindowAvailable)
			}

			session.Lock()
			shouldFlush := false
			c := int(session.sendMtu)
			l := len(session.sendBuffer)
			if n >= c-l {
				n = c - l
				shouldFlush = true
			}
			session.sendBuffer = session.sendBuffer[:l+n]
			copy(session.sendBuffer[l:], b)
			session.sendWindowUsed += uint32(n)
			bytesSent += n
			session.Unlock()

			if shouldFlush {
				err = session.flushSendBuffer()
				if err != nil {
					return bytesSent, err
				}
			}
			b = b[n:]
		}
	} else {
		_, err := session.waitForSendWindow(session.writeContext, uint32(len(b)))
		if err != nil {
			return bytesSent, err
		}

		session.Lock()
		session.sendBuffer = session.sendBuffer[:len(b)]
		copy(session.sendBuffer, b)
		session.sendWindowUsed += uint32(len(b))
		bytesSent += len(b)
		session.Unlock()

		err = session.flushSendBuffer()
		if err != nil {
			return bytesSent, err
		}
	}

	return bytesSent, nil
}

func (session *Session) close() {
	session.Lock()
	session.isClosed = true
	session.Unlock()

	session.cancel()
	session.readCancel()
	session.writeCancel()
}

func (session *Session) Close() error {
	go func() {
		err := session.flushSendBuffer()
		if err != nil {
			log.Println(err)
		}
		err = session.sendClosePacket()
		if err != nil {
			log.Println(err)
		}
	}()
	time.AfterFunc(session.config.CloseDelay, session.close)
	return nil
}

func (session *Session) LocalAddr() net.Addr {
	return session.localAddr
}

func (session *Session) RemoteAddr() net.Addr {
	return session.remoteAddr
}

func (session *Session) SetDeadline(t time.Time) error {
	err := session.SetReadDeadline(t)
	if err != nil {
		return err
	}
	err = session.SetWriteDeadline(t)
	if err != nil {
		return err
	}
	return nil
}

func (session *Session) SetReadDeadline(t time.Time) error {
	if t == zeroTime {
		session.readContext, session.readCancel = context.WithCancel(session.ctx)
	} else {
		session.readContext, session.readCancel = context.WithDeadline(session.ctx, t)
	}
	return nil
}

func (session *Session) SetWriteDeadline(t time.Time) error {
	if t == zeroTime {
		session.writeContext, session.writeCancel = context.WithCancel(session.ctx)
	} else {
		session.writeContext, session.writeCancel = context.WithDeadline(session.ctx, t)
	}
	return nil
}
