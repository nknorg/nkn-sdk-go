package examples

import (
	"crypto/rand"
	"fmt"
	"log"
	"testing"
	"time"

	"github.com/nknorg/nkn/common"
	"github.com/nknorg/nkn/vault"
	. "github.com/nknorg/nkn-sdk-go"
)

func TestClient(t *testing.T) {
	err := func () error {
		Init()

		privateKey, _ := common.HexStringToBytes("039e481266e5a05168c1d834a94db512dbc235877f150c5a3cc1e3903672c67352dff44c21790d9edef7a7e3fc9bd7254359246d0ae605a3c97e71aad83d6b0d")
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

		err = fromClient.Send([]string{toClient.Address}, []byte("Hello world!"))
		if err != nil {
			return err
		}
		timeSent := time.Now().UnixNano() / int64(time.Millisecond)
		log.Println("Send message from", fromClient.Address, "to", toClient.Address)

		msg := <- toClient.OnMessage
		timeReceived := time.Now().UnixNano() / int64(time.Millisecond)
		log.Println("Receive message", "\"" + string(msg.Payload) + "\"", "from", msg.Src, "after", timeReceived - timeSent, "ms")

		// wait to send receipt
		time.Sleep(time.Second)

		return nil
	}()
	if err != nil {
		fmt.Println(err)
	}
}
