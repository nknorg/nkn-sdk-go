package nkn_sdk_go

import (
	"container/heap"
	"context"
	"log"
	"math"
	"sync"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/nknorg/nkn-sdk-go/payloads"
)

type Connection struct {
	session          *Session
	clientID         string
	remoteAddr       string
	windowSize       uint32
	sendWindowUpdate chan struct{}

	sync.RWMutex
	timeSentSeq           map[uint32]time.Time
	resentSeq             map[uint32]struct{}
	sendAckQueue          SeqHeap
	retransmissionTimeout time.Duration
}

func (session *Session) NewConnection(clientID, remoteAddr string) (*Connection, error) {
	conn := &Connection{
		session:               session,
		clientID:              clientID,
		remoteAddr:            remoteAddr,
		windowSize:            session.config.InitialConnectionWindowSize,
		retransmissionTimeout: session.config.InitialRetransmissionTimeout,
		sendWindowUpdate:      make(chan struct{}, 1),
		timeSentSeq:           make(map[uint32]time.Time),
		resentSeq:             make(map[uint32]struct{}),
		sendAckQueue:          make(SeqHeap, 0),
	}
	heap.Init(&conn.sendAckQueue)
	return conn, nil
}

func (conn *Connection) SendWindowUsed() uint32 {
	conn.RLock()
	defer conn.RUnlock()
	return uint32(len(conn.timeSentSeq))
}

func (conn *Connection) RetransmissionTimeout() time.Duration {
	conn.RLock()
	defer conn.RUnlock()
	return conn.retransmissionTimeout
}

func (conn *Connection) SendACK(sequenceID uint32) {
	conn.Lock()
	heap.Push(&conn.sendAckQueue, sequenceID)
	conn.Unlock()
}

func (conn *Connection) SendAckQueueLen() int {
	conn.RLock()
	defer conn.RUnlock()
	return conn.sendAckQueue.Len()
}

func (conn *Connection) ReceiveACK(sequenceID uint32) {
	conn.Lock()
	defer conn.Unlock()
	if t, ok := conn.timeSentSeq[sequenceID]; ok {
		if _, ok := conn.resentSeq[sequenceID]; !ok {
			conn.windowSize++
			if conn.windowSize > conn.session.config.MaxConnectionWindowSize {
				conn.windowSize = conn.session.config.MaxConnectionWindowSize
			}
		}
		conn.retransmissionTimeout += time.Duration(math.Tanh(float64(3*time.Since(t)-conn.retransmissionTimeout)/float64(time.Millisecond)/1000) * 100 * float64(time.Millisecond))
		if conn.retransmissionTimeout > conn.session.config.MaxRetransmissionTimeout {
			conn.retransmissionTimeout = conn.session.config.MaxRetransmissionTimeout
		}
		delete(conn.timeSentSeq, sequenceID)
		delete(conn.resentSeq, sequenceID)
		select {
		case conn.sendWindowUpdate <- struct{}{}:
		default:
		}
	}
}

func (conn *Connection) waitForSendWindow(ctx context.Context) error {
	for conn.SendWindowUsed() >= conn.windowSize {
		select {
		case <-conn.sendWindowUpdate:
		case <-time.After(maxWait):
			return errMaxWait
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (conn *Connection) Start() {
	go conn.tx()
	go conn.sendAck()
	go conn.checkTimeout()
}

func (conn *Connection) tx() error {
	var seq uint32
	var err error
	for {
		if seq == 0 {
			seq, err = conn.session.GetResendSeq()
			if err != nil {
				return err
			}
		}
		if seq == 0 {
			err = conn.waitForSendWindow(conn.session.ctx)
			if err == errMaxWait {
				continue
			}
			if err != nil {
				return err
			}

			seq, err = conn.session.GetSendSeq()
			if err != nil {
				return err
			}
		}

		buf := conn.session.GetDataToSend(seq)
		if len(buf) == 0 {
			conn.Lock()
			delete(conn.timeSentSeq, seq)
			delete(conn.resentSeq, seq)
			conn.Unlock()
			seq = 0
			continue
		}

		err = conn.session.sendWith(conn.clientID, conn.remoteAddr, buf, conn.retransmissionTimeout)
		if err != nil {
			if conn.session.IsClosed() {
				return Closed
			}
			log.Println(err)
			select {
			case conn.session.resendChan <- seq:
				seq = 0
			default:
				log.Println("Resend channel full")
			}
			time.Sleep(time.Second)
			continue
		}

		conn.Lock()
		if _, ok := conn.timeSentSeq[seq]; !ok {
			conn.timeSentSeq[seq] = time.Now()
		}
		delete(conn.resentSeq, seq)
		conn.Unlock()

		seq = 0
	}
}

func (conn *Connection) sendAck() error {
	for {
		select {
		case <-time.After(conn.session.config.SendAckInterval):
		case <-conn.session.ctx.Done():
			return conn.session.ctx.Err()
		}

		if conn.SendAckQueueLen() == 0 {
			continue
		}

		ackStartSeqList := make([]uint32, 0)
		ackSeqCountList := make([]uint32, 0)

		conn.Lock()
		for conn.sendAckQueue.Len() > 0 && len(ackStartSeqList) < int(conn.session.config.MaxAckSeqListSize) {
			ackStartSeq := heap.Pop(&conn.sendAckQueue).(uint32)
			ackSeqCount := uint32(1)
			for conn.sendAckQueue.Len() > 0 && conn.sendAckQueue[0] == NextSeq(ackStartSeq, ackSeqCount) {
				heap.Pop(&conn.sendAckQueue)
				ackSeqCount++
			}

			ackStartSeqList = append(ackStartSeqList, ackStartSeq)
			ackSeqCountList = append(ackSeqCountList, ackSeqCount)
		}
		conn.Unlock()

		omitCount := true
		for _, c := range ackSeqCountList {
			if c != 1 {
				omitCount = false
				break
			}
		}
		if omitCount {
			ackSeqCountList = nil
		}

		buf, err := proto.Marshal(&payloads.SessionData{
			AckStartSeq: ackStartSeqList,
			AckSeqCount: ackSeqCountList,
		})
		if err != nil {
			log.Println(err)
			continue
		}

		err = conn.session.sendWith(conn.clientID, conn.remoteAddr, buf, conn.retransmissionTimeout)
		if err != nil {
			log.Println(err)
			time.Sleep(time.Second)
			continue
		}
	}
}

func (conn *Connection) checkTimeout() error {
	for {
		select {
		case <-time.After(conn.session.config.CheckTimeoutInterval):
		case <-conn.session.ctx.Done():
			return conn.session.ctx.Err()
		}

		threshold := time.Now().Add(-conn.retransmissionTimeout)
		conn.Lock()
		for seq, t := range conn.timeSentSeq {
			if _, ok := conn.resentSeq[seq]; ok {
				continue
			}
			if t.Before(threshold) {
				select {
				case conn.session.resendChan <- seq:
					conn.resentSeq[seq] = struct{}{}
					conn.windowSize /= 2
					if conn.windowSize < conn.session.config.MinConnectionWindowSize {
						conn.windowSize = conn.session.config.MinConnectionWindowSize
					}
				default:
					log.Println("Resend channel full, discarding message")
				}
			}
		}
		conn.Unlock()
	}
}
