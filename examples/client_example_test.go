package examples

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"testing"
	"time"

	nkn "github.com/nknorg/nkn-sdk-go"
)

func TestClient(t *testing.T) {
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

		fromClient, err := nkn.NewMultiClient(account, hex.EncodeToString(fromIdentifier), 0, true, nil)
		if err != nil {
			return err
		}
		defer fromClient.Close()
		<-fromClient.OnConnect.C

		toClient, err := nkn.NewMultiClient(account, hex.EncodeToString(toIdentifier), 0, true, nil)
		if err != nil {
			return err
		}
		defer toClient.Close()
		<-toClient.OnConnect.C

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
			msg.Reply([]byte("World"))
		}()

		log.Println("Send message from", fromClient.Address(), "to", toClient.Address())
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
