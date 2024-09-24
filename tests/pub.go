package tests

import (
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/nknorg/nkn-sdk-go"
)

func CreateClientConfig(numClients int, retries int32, webrtc bool) (config *nkn.ClientConfig) {
	config = &nkn.ClientConfig{
		MultiClientNumClients: numClients,
		ConnectRetries:        retries,
		WebRTC:                webrtc,
	}
	return
}

func CreateMessageConfig(maxHoldingSeconds int32) (config *nkn.MessageConfig) {
	config = &nkn.MessageConfig{MaxHoldingSeconds: maxHoldingSeconds}
	return
}

func CreateMultiClient(id, seed string, numClient int, connectFailRate int, webrtc bool) (mc *nkn.MultiClient, err error) {
	clientConfig := CreateClientConfig(numClients, 5, webrtc)
	var byteSeed []byte
	if len(seed) > 0 {
		byteSeed, _ = hex.DecodeString(seed)
	}
	acc, err := nkn.NewAccount(byteSeed)
	if err != nil {
		log.Printf("nkn.NewAccount err %v", err)
		return
	}

	// clientConfig.ConnectFailRate = connectFailRate
	mc, err = nkn.NewMultiClientV2(acc, id, clientConfig)
	if err != nil {
		return
	}

	<-mc.OnConnect.C

	return
}

func CreateClient(id, seed string, webrtc bool) (client *nkn.Client, err error) {
	clientConfig := CreateClientConfig(0, 5, webrtc)
	var byteSeed []byte
	if len(seed) > 0 {
		byteSeed, _ = hex.DecodeString(seed)
	}
	acc, err := nkn.NewAccount(byteSeed)
	if err != nil {
		log.Printf("nkn.NewAccount err %v", err)
		return
	}

	client, err = nkn.NewClient(acc, id, clientConfig)
	if err != nil {
		log.Fatal(err)
	}
	if node, ok := <-client.OnConnect.C; ok {
		log.Printf("%v connects to NKN node %v, address is: %v", id, node.Addr, client.Address())
		return
	}

	return nil, errors.New("create client fail")
}

func mcrecv(client *nkn.MultiClient, name string) (recv int, unreceived []int) {
	recved := make(map[int]int)
	gotExit := 0
	for {
		msg := client.OnMessage.NextWithTimeout(recvMsgTimeout)
		if msg == nil {
			break
		}
		if string(msg.Data) == exitMsg {
			gotExit++
		} else {
			if recvedNum, err := strconv.Atoi(string(msg.Data)); err == nil {
				if _, ok := recved[recvedNum]; !ok { // 避开重复收取的计数
					recved[recvedNum] = 1 // record it.
					log.Printf("%v recv: %v", name, recvedNum)
					recv++
				}
			}
		}

		if gotExit >= numExitMsgs/2 && len(recved) >= numMsgs {
			break
		}
	}

	if len(recved) < numMsgs {
		for i := 0; i < numMsgs; i++ {
			if _, ok := recved[i]; !ok {
				unreceived = append(unreceived, i)
			}
		}
		log.Printf("%v recved %v, gotExit %v,  unreceived: %+v", client.Address(), recv, gotExit, unreceived)
	}

	return
}

func recv(client *nkn.Client, name string, restart bool) (recv int, unreceived []int) {
	recved := make(map[int]int)
	gotExit := 0
	for {
		msg := client.OnMessage.NextWithTimeout(recvMsgTimeout)
		if msg == nil {
			break
		}
		if string(msg.Data) == exitMsg {
			gotExit++
		} else {
			if recvedNum, err := strconv.Atoi(string(msg.Data)); err == nil {
				if _, ok := recved[recvedNum]; !ok {
					recved[recvedNum] = 1 // record it.
					log.Printf("%v recv: %v", name, recvedNum)
					recv++
				}
			}
		}

		if gotExit >= numExitMsgs/2 && len(recved) >= numMsgs {
			break
		}

		if restart && recv == numMsgs/2 {
			client.GetConn().Close()
			log.Printf("%v connection is closed", name)
		}
	}

	if len(recved) < numMsgs {
		for i := 0; i < numMsgs; i++ {
			if _, ok := recved[i]; !ok {
				unreceived = append(unreceived, i)
			}
		}
		log.Printf("%v recved %v, gotExit %v,  unreceived: %+v", client.Address(), recv, gotExit, unreceived)
	}

	return
}

func send(client *nkn.Client, peer, name string, ch chan int) (sent, sendFail int) {
	sent = 0
	msgConfig := CreateMessageConfig(600)

	for i := 1; i <= numMsgs; i++ {
		select {
		case <-ch: // receiver exit, no need send any more
			return
		default:
		}

		msg := fmt.Sprintf("%v", i)
		_, err := client.SendText(nkn.NewStringArray(peer), msg, msgConfig)
		if err != nil {
			log.Printf("%v.SendText err %v\n", name, err)
			sendFail++
			if sendFail > numMsgs/2 {
				break
			}
			continue
		} else {
			log.Printf("%v sent: %v", name, msg)
			sent++
		}
		time.Sleep(100 * time.Millisecond)
	}

	for i := 0; i < numExitMsgs; i++ {
		_, err := client.SendText(nkn.NewStringArray(peer), exitMsg, nil)
		if err != nil {
			log.Printf("%v SendText exit err %v", name, err)
		}
	}

	log.Printf("%v sent success: %v, fail: %v\n", name, sent, sendFail)

	return
}

func mcsend(client *nkn.MultiClient, peer, name string, ch chan int) (sent, sendFail int) {
	sent = 0
	msgConfig := CreateMessageConfig(600)

	for i := 0; i <= numMsgs; i++ {
		select {
		case <-ch: // receiver exit, no need send any more
			return
		default:
		}

		msg := fmt.Sprintf("%v", i)
		_, err := client.SendText(nkn.NewStringArray(peer), msg, msgConfig)
		if err != nil {
			log.Printf("%v SendText err %v\n", name, err)
			sendFail++
			if sendFail > numMsgs/2 {
				break
			}
			continue
		} else {
			log.Printf("%v sent: %v", name, msg)
			sent++
		}
		time.Sleep(100 * time.Millisecond)
	}

	for i := 0; i < numExitMsgs; i++ {
		// 多发几次exit，让接收端有机会收到更多的测试消息再退出
		// Send exit a few more times to give the receiver a chance to receive more test messages before exiting
		_, err := client.SendText(nkn.NewStringArray(peer), exitMsg, nil)
		if err != nil {
			log.Printf("%v SendText exit err: %v", name, err)
		}
	}

	log.Printf("%v %v sent %v, sendFail %v\n", time.Now(), name, sent, sendFail)

	return
}
