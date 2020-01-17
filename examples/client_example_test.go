package examples

import (
	"crypto/rand"
	"fmt"
	"log"
	"testing"
	"time"

	nknsdk "github.com/nknorg/nkn-sdk-go"
	"github.com/nknorg/nkn/common"
	"github.com/nknorg/nkn/crypto"
	"github.com/nknorg/nkn/vault"
)

func TestClient(t *testing.T) {
	err := func() error {
		seed, _ := common.HexStringToBytes("039e481266e5a05168c1d834a94db512dbc235877f150c5a3cc1e3903672c673")
		privateKey := crypto.GetPrivateKeyFromSeed(seed)
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

		fromClient, err := nknsdk.NewMultiClient(account, common.BytesToHexString(fromIdentifier), 0, true)
		if err != nil {
			return err
		}
		defer fromClient.Close()
		<-fromClient.OnConnect

		toClient, err := nknsdk.NewMultiClient(account, common.BytesToHexString(toIdentifier), 0, true)
		if err != nil {
			return err
		}
		defer toClient.Close()
		<-toClient.OnConnect

		timeSent := time.Now().UnixNano() / int64(time.Millisecond)
		var timeReceived int64
		go func() {
			msg := <-toClient.OnMessage
			timeReceived = time.Now().UnixNano() / int64(time.Millisecond)
			isEncryptedStr := "unencrypted"
			if msg.Encrypted {
				isEncryptedStr = "encrypted"
			}
			log.Println("Receive", isEncryptedStr, "message", "\""+string(msg.Data)+"\"", "from", msg.Src, "after", timeReceived-timeSent, "ms")
			msg.Reply([]byte("World"))
		}()

		log.Println("Send message from", fromClient.Address, "to", toClient.Address)
		respChan, err := fromClient.Send([]string{toClient.Address}, []byte("Hello"))
		if err != nil {
			return err
		}
		response := <-respChan
		isEncryptedStr := "unencrypted"
		if response.Encrypted {
			isEncryptedStr = "encrypted"
		}
		timeResponse := time.Now().UnixNano() / int64(time.Millisecond)
		log.Println("Got", isEncryptedStr, "response", "\""+string(response.Data)+"\"", "from", response.Src, "after", timeResponse-timeReceived, "ms")

		// wait to send receipt
		time.Sleep(time.Second)

		return nil
	}()
	if err != nil {
		fmt.Println(err)
	}
}
