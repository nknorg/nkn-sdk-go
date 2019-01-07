package main

import (
	"crypto/rand"
	"fmt"
	. "github.com/nknorg/nkn-sdk-go"
	"github.com/nknorg/nkn/common"
	"github.com/nknorg/nkn/vault"
	"log"
	"time"
)

func main() {
	err := func () error {
		Init()

		privateKey, _ := common.HexStringToBytes("cd5fa29ed5b0e951f3d1bce5997458706186320f1dd89156a73d54ed752a7f37")
		account, err := vault.NewAccountWithPrivatekey(privateKey)
		if err != nil {
			return err
		}

		fromIdentifier := make([]byte, 8)
		_, err = rand.Read(fromIdentifier)
		if err != nil {
			return err
		}
		toIdentifier := make([]byte, 8)
		_, err = rand.Read(toIdentifier)
		if err != nil {
			return err
		}

		fromClient, err := NewClient(account, common.BytesToHexString(fromIdentifier))
		if err != nil {
			return err
		}
		defer fromClient.Close()
		<- fromClient.OnConnect

		toClient, err := NewClient(account, common.BytesToHexString(toIdentifier))
		if err != nil {
			return err
		}
		defer toClient.Close()
		<- toClient.OnConnect

		err = fromClient.Send([]string{toClient.Address}, []byte("Hello world!"), 0)
		if err != nil {
			return err
		}
		timeSent := time.Now().UnixNano() / int64(time.Millisecond)
		log.Println("Send message from", fromClient.Address, "to", toClient.Address)

		msg := <- toClient.OnMessage
		timeReceived := time.Now().UnixNano() / int64(time.Millisecond)
		log.Println("Receive message", "\"" + string(msg.Payload) + "\"", "from", msg.Src, "after", timeReceived - timeSent, "ms")

		return nil
	}()
	if err != nil {
		fmt.Println(err)
	}
}
