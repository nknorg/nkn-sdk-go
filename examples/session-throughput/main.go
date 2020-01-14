package main

import (
	"encoding/binary"
	"encoding/hex"
	"flag"
	"log"
	"math"
	"net"
	"os"
	"strings"
	"time"

	nknsdk "github.com/nknorg/nkn-sdk-go"
	"github.com/nknorg/nkn/common"
	"github.com/nknorg/nkn/crypto"
	"github.com/nknorg/nkn/vault"
)

const (
	dialID   = "alice"
	listenID = "bob"
)

func read(sess net.Conn) error {
	timeStart := time.Now()

	b := make([]byte, 4)
	n := 0
	for {
		m, err := sess.Read(b[n:])
		if err != nil {
			return err
		}
		n += m
		if n == 4 {
			break
		}
	}

	numBytes := int(binary.LittleEndian.Uint32(b))

	b = make([]byte, 1024)
	bytesReceived := 0
	for {
		n, err := sess.Read(b)
		if err != nil {
			return err
		}
		bytesReceived += n
		if ((bytesReceived - n) * 10 / numBytes) != (bytesReceived * 10 / numBytes) {
			log.Println("Received", bytesReceived, "bytes", float64(bytesReceived)/math.Pow(2, 20)/(float64(time.Since(timeStart))/float64(time.Second)), "MB/s")
		}
		if bytesReceived == numBytes {
			log.Println("Finished receiving", bytesReceived, "bytes")
			return nil
		}
	}
}

func write(sess net.Conn, numBytes int) error {
	timeStart := time.Now()

	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(numBytes))
	_, err := sess.Write(b)
	if err != nil {
		return err
	}

	bytesSent := 0
	for i := 0; i < numBytes/1024; i++ {
		b := make([]byte, 1024)
		for j := 0; j < len(b); j++ {
			b[j] = byte(j % math.MaxUint8)
		}
		n, err := sess.Write(b)
		if err != nil {
			return err
		}
		bytesSent += n
		if ((bytesSent - n) * 10 / numBytes) != (bytesSent * 10 / numBytes) {
			log.Println("Sent", bytesSent, "bytes", float64(bytesSent)/math.Pow(2, 20)/(float64(time.Since(timeStart))/float64(time.Second)), "MB/s")
		}
	}
	return nil
}

func main() {
	numClients := flag.Int("n", 1, "number of clients")
	numBytes := flag.Int("m", 1, "data to send (MB)")
	seedHex := flag.String("s", "", "secret seed")
	dialAddr := flag.String("a", "", "dial address")
	dial := flag.Bool("d", false, "dial")
	listen := flag.Bool("l", false, "listen")

	flag.Parse()

	*numBytes *= 1 << 20

	var account *vault.Account
	var err error
	if len(*seedHex) > 0 {
		seed, err := common.HexStringToBytes(*seedHex)
		if err != nil {
			log.Fatal(err)
		}
		account, err = vault.NewAccountWithPrivatekey(crypto.GetPrivateKeyFromSeed(seed))
		if err != nil {
			log.Fatal(err)
		}
	} else {
		account, err = vault.NewAccount()
		if err != nil {
			log.Fatal(err)
		}
	}

	log.Println("Seed:", hex.EncodeToString(account.PrivateKey[:32]))

	if *listen {
		m, err := nknsdk.NewMultiClient(account, listenID, *numClients, false, nknsdk.ClientConfig{ConnectRetries: 1})
		if err != nil {
			log.Fatal(err)
		}

		go func() {
			for {
				s, err := m.Accept()
				if err != nil {
					log.Fatal(err)
				}
				log.Println(m.Addr(), "accepted a session")

				go func(s net.Conn) {
					err := read(s)
					if err != nil {
						log.Fatal(err)
					}
					s.Close()
				}(s)
			}
		}()
	}

	if *dial {
		m, err := nknsdk.NewMultiClient(account, dialID, *numClients, false, nknsdk.ClientConfig{ConnectRetries: 1})
		if err != nil {
			log.Fatal(err)
		}

		if len(*dialAddr) == 0 {
			*dialAddr = listenID + "." + strings.SplitN(m.Address, ".", 2)[1]
		}

		s, err := m.DialWithConfig(*dialAddr, &nknsdk.SessionConfig{DialTimeout: 3 * time.Second})
		if err != nil {
			log.Fatal(err)
		}
		log.Println(m.Addr(), "dialed a session")

		go func() {
			err := write(s, *numBytes)
			if err != nil {
				log.Fatal(err)
			}
			for {
				if s.IsClosed() {
					os.Exit(0)
				}
				time.Sleep(time.Millisecond * 100)
			}
		}()
	}

	select {}
}
