package nkn_sdk_go

import (
	"crypto/rand"
	cryptorand "crypto/rand"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	zeroTime time.Time
)

type Addr struct {
	addr string
}

func (addr *Addr) Network() string { return "nkn" }
func (addr *Addr) String() string  { return addr.addr }

type SeqHeap []uint32

func (h SeqHeap) Len() int           { return len(h) }
func (h SeqHeap) Less(i, j int) bool { return CompareSeq(h[i], h[j]) < 0 }
func (h SeqHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *SeqHeap) Push(x interface{}) {
	*h = append(*h, x.(uint32))
}

func (h *SeqHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

func PrevSeq(seq, step uint32) uint32 {
	return (seq-MinSequenceID-step)%(math.MaxUint32-MinSequenceID+1) + MinSequenceID
}

func NextSeq(seq, step uint32) uint32 {
	return (seq-MinSequenceID+step)%(math.MaxUint32-MinSequenceID+1) + MinSequenceID
}

func SeqInBetween(startSeq, endSeq, targetSeq uint32) bool {
	if startSeq <= endSeq {
		return targetSeq >= startSeq && targetSeq < endSeq
	}
	return targetSeq >= startSeq || targetSeq < endSeq
}

func CompareSeq(seq1, seq2 uint32) int {
	if seq1 == seq2 {
		return 0
	}
	if seq1 < seq2 {
		if seq2-seq1 < math.MaxUint32/2 {
			return -1
		}
		return 1
	} else {
		if seq1-seq2 < math.MaxUint32/2 {
			return 1
		}
		return -1
	}
}

func addIdentifierPrefix(base, prefix string) string {
	if len(base) == 0 {
		return prefix
	}
	if len(prefix) == 0 {
		return base
	}
	return prefix + "." + base
}

func addIdentifier(addr string, id int) string {
	if id < 0 {
		return addr
	}
	return addIdentifierPrefix(addr, "__"+strconv.Itoa(id)+"__")
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

func connKey(clientID, remoteAddr string) string {
	return clientID + "-" + remoteAddr
}

func randUint32() uint32 {
	max := big.NewInt(4294967296)
	for {
		result, err := cryptorand.Int(cryptorand.Reader, max)
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
		result, err := cryptorand.Int(cryptorand.Reader, max)
		if err != nil {
			continue
		}
		return result.Uint64()
	}
}
