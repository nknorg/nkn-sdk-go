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

		cfg := &nkn.ClientConfig{
			MultiClientOriginalClient: true,
			MultiClientNumClients:     5,
			MessageConfig: &nkn.MessageConfig{
				MaxHoldingSeconds: 86400,
			},
		}

		fromClient, err := nkn.NewMultiClientV2(account, hex.EncodeToString(fromIdentifier), cfg)
		if err != nil {
			return err
		}
		defer fromClient.Close()
		<-fromClient.OnConnect.C

		toClient, err := nkn.NewMultiClientV2(account, hex.EncodeToString(toIdentifier), cfg)
		if err != nil {
			return err
		}
		defer toClient.Close()
		<-toClient.OnConnect.C

		// Demo: Monitor message events for fromClient (sender)
		go func() {
			for event := range fromClient.OnMessageEvent.C {
				switch event.Type {
				case nkn.MessageEventTypeSend:
					subClientInfo := ""
					if event.SubClientID != nil {
						subClientInfo = fmt.Sprintf(" (subclient[%d])", *event.SubClientID)
					}
					log.Printf("[Event] Send attempt: client=%s%s, destinations=%v, messageID=%x, encrypted=%v",
						event.ClientAddr, subClientInfo, event.Destinations, event.MessageID, event.Encrypted)
				case nkn.MessageEventTypeSendSuccess:
					subClientInfo := ""
					if event.SubClientID != nil {
						subClientInfo = fmt.Sprintf(" (subclient[%d])", *event.SubClientID)
					}
					log.Printf("[Event] Send success: client=%s%s, messageID=%x",
						event.ClientAddr, subClientInfo, event.MessageID)
				case nkn.MessageEventTypeSendFailed:
					subClientInfo := ""
					if event.SubClientID != nil {
						subClientInfo = fmt.Sprintf(" (subclient[%d])", *event.SubClientID)
					}
					log.Printf("[Event] Send failed: client=%s%s, messageID=%x, error=%v",
						event.ClientAddr, subClientInfo, event.MessageID, event.Error)
				case nkn.MessageEventTypeReceiveReply:
					subClientInfo := ""
					if event.SubClientID != nil {
						subClientInfo = fmt.Sprintf(" (subclient[%d])", *event.SubClientID)
					}
					log.Printf("[Event] Receive reply: client=%s%s, from=%s, messageID=%x",
						event.ClientAddr, subClientInfo, event.Src, event.MessageID)
				}
			}
		}()

		// Demo: Monitor message events for toClient (receiver)
		go func() {
			for event := range toClient.OnMessageEvent.C {
				switch event.Type {
				case nkn.MessageEventTypeReceive:
					subClientInfo := ""
					if event.SubClientID != nil {
						subClientInfo = fmt.Sprintf(" (subclient[%d])", *event.SubClientID)
					}
					log.Printf("[Event] Receive: client=%s%s, from=%s, messageID=%x, type=%d, encrypted=%v, dataSize=%d",
						event.ClientAddr, subClientInfo, event.Src, event.MessageID, event.MessageType, event.Encrypted, event.DataSize)
				case nkn.MessageEventTypeReceiveReply:
					subClientInfo := ""
					if event.SubClientID != nil {
						subClientInfo = fmt.Sprintf(" (subclient[%d])", *event.SubClientID)
					}
					log.Printf("[Event] Receive reply: client=%s%s, from=%s, messageID=%x",
						event.ClientAddr, subClientInfo, event.Src, event.MessageID)
				}
			}
		}()

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
		time.Sleep(time.Second * 3)

		return nil
	}()
	if err != nil {
		fmt.Println(err)
	}
}
