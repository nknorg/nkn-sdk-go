package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"github.com/nknorg/nkn-sdk-go"
)

func main() {
	err := func() error {
		account, err := nkn.NewAccount(nil)
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

		fromClient, err := nkn.NewMultiClientV2(account, hex.EncodeToString(fromIdentifier), nil)
		if err != nil {
			return err
		}
		defer fromClient.Close()
		<-fromClient.OnConnect.C

		toClient, err := nkn.NewMultiClientV2(account, hex.EncodeToString(toIdentifier), nil)
		if err != nil {
			return err
		}
		defer toClient.Close()
		<-toClient.OnConnect.C

		time.Sleep(time.Second)

		timeSent := time.Now().UnixNano() / int64(time.Millisecond)
		var timeReceived int64
		go func() {
			msg := <-toClient.OnMessage.C
			timeReceived = time.Now().UnixNano() / int64(time.Millisecond)
			isEncryptedStr := "unencrypted"
			if msg.Encrypted {
				isEncryptedStr = "encrypted"
			}
			log.Println("Receive", isEncryptedStr, "message", "\""+string(msg.Data)+"\"", "from", msg.Src, "after", timeReceived-timeSent, "ms")
			// []byte("World") can be replaced with "World" for text payload type
			msg.Reply([]byte("World"))
		}()

		log.Println("Send message from", fromClient.Address(), "to", toClient.Address())
		// []byte("Hello") can be replaced with "Hello" for text payload type
		onReply, err := fromClient.Send(nkn.NewStringArray(toClient.Address()), []byte("Hello"), nil)
		if err != nil {
			return err
		}
		reply := <-onReply.C
		isEncryptedStr := "unencrypted"
		if reply.Encrypted {
			isEncryptedStr = "encrypted"
		}
		timeResponse := time.Now().UnixNano() / int64(time.Millisecond)
		log.Println("Got", isEncryptedStr, "reply", "\""+string(reply.Data)+"\"", "from", reply.Src, "after", timeResponse-timeReceived, "ms")

		// wait to send receipt
		time.Sleep(time.Second)

		return nil
	}()
	if err != nil {
		fmt.Println(err)
	}
}
