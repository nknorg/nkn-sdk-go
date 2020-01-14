package main

import (
	"encoding/hex"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"strings"

	nknsdk "github.com/nknorg/nkn-sdk-go"
	"github.com/nknorg/nkn/common"
	"github.com/nknorg/nkn/crypto"
	"github.com/nknorg/nkn/vault"
)

const (
	dialID   = "alice"
	listenID = "bob"
)

func main() {
	numClients := flag.Int("n", 1, "number of clients")
	seedHex := flag.String("s", "", "secret seed")
	dialAddr := flag.String("a", "", "dial address")
	dial := flag.Bool("d", false, "dial")
	listen := flag.Bool("l", false, "listen")
	serveDir := flag.String("dir", ".", "serve directory")
	httpAddr := flag.String("http", ":8080", "http listen address")

	flag.Parse()

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
			log.Println("Serving content at", m.Address)
			fs := http.FileServer(http.Dir(*serveDir))
			http.Handle("/", fs)
			http.Serve(m, nil)
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

		listener, err := net.Listen("tcp", *httpAddr)
		if err != nil {
			log.Fatal(err)
		}

		log.Println("Http server listening at", *httpAddr)

		go func() {
			for {
				c, err := listener.Accept()
				if err != nil {
					log.Fatal(err)
				}

				s, err := m.Dial(*dialAddr)
				if err != nil {
					log.Fatal(err)
				}

				go func() {
					io.Copy(c, s)
				}()
				go func() {
					io.Copy(s, c)
				}()
			}
		}()
	}

	select {}
}
