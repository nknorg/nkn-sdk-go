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
		// Create account
		account, err := nkn.NewAccount(nil)
		if err != nil {
			return err
		}

		// Generate random identifiers
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

		// ============================================
		// Step 1: Create receiver client first
		// ============================================
		log.Println("=== Step 1: Creating Receiver Client ===")
		toClient, err := nkn.NewMultiClientV2(account, hex.EncodeToString(toIdentifier), &nkn.ClientConfig{
			MultiClientOriginalClient: true,
			MultiClientNumClients:     4,
		})
		if err != nil {
			return err
		}
		defer toClient.Close()

		// Wait for receiver client to connect
		log.Println("Waiting for receiver client to connect...")
		select {
		case <-toClient.OnConnect.C:
			log.Println("Receiver client connected")
		case <-time.After(30 * time.Second):
			return fmt.Errorf("timeout waiting for receiver client connection")
		}
		time.Sleep(time.Second)

		// Set up message receiver - use a loop to handle multiple messages
		var timeReceived int64
		messageCount := 0
		go func() {
			for {
				msg, ok := <-toClient.OnMessage.C
				if !ok {
					return
				}
				messageCount++
				timeReceived = time.Now().UnixNano() / int64(time.Millisecond)
				isEncryptedStr := "unencrypted"
				if msg.Encrypted {
					isEncryptedStr = "encrypted"
				}
				log.Printf("Receiver: Received %s message #%d \"%s\" from %s",
					isEncryptedStr, messageCount, string(msg.Data), msg.Src)
				msg.Reply([]byte("World"))
			}
		}()

		// ============================================
		// Step 2: Create sender client with cross send policy
		// ============================================
		log.Println("\n=== Step 2: Creating Sender Client with Cross Send Policy ===")
		// Note: MultiClientOriginalClient creates an additional client without identifier prefix
		// MultiClientNumClients creates sub-clients with identifier prefixes (__0__, __1__, etc.)
		config := &nkn.ClientConfig{
			MultiClientOriginalClient: true,
			MultiClientNumClients:     4,
			CrossSendPolicy:           nkn.CrossSendPolicyAnyConnected,
		}

		fromClient, err := nkn.NewMultiClientV2(account, hex.EncodeToString(fromIdentifier), config)
		if err != nil {
			return err
		}
		defer fromClient.Close()

		// Wait for sender client to connect
		log.Println("Waiting for sender client to connect...")
		select {
		case <-fromClient.OnConnect.C:
			log.Println("Sender client connected")
		case <-time.After(30 * time.Second):
			return fmt.Errorf("timeout waiting for sender client connection")
		}

		// Wait for all sub-clients to connect
		log.Println("Waiting for all sub-clients to connect...")
		maxWaitTime := 10 * time.Second
		checkInterval := 500 * time.Millisecond
		startTime := time.Now()
		for {
			states := fromClient.GetAllSubConnStates()
			connectedCount := 0
			for _, state := range states {
				if state.State == nkn.ConnConnected {
					connectedCount++
				}
			}
			if connectedCount > 0 || time.Since(startTime) > maxWaitTime {
				break
			}
			time.Sleep(checkInterval)
		}
		time.Sleep(time.Second)

		// ============================================
		// Step 3: Display all sub-connection states
		// ============================================
		log.Println("\n=== Step 3: Sub-Connection States ===")
		states := fromClient.GetAllSubConnStates()
		log.Printf("Total sub-clients: %d\n", len(states))
		for _, state := range states {
			stateStr := getStateString(state.State)
			log.Printf("  Client[%d]:", state.Index)
			log.Printf("    State: %s", stateStr)
			log.Printf("    SubAddress: %s", state.SubAddress)
			log.Printf("    BaseAddress: %s", state.BaseAddress)
			if state.NodeAddr != "" {
				log.Printf("    NodeAddr: %s", state.NodeAddr)
				log.Printf("    NodeID: %s", state.NodeID)
			}
			if state.Err != nil {
				log.Printf("    Error: %v", state.Err)
			}
			log.Println()
		}

		// Count connected clients
		connectedCount := 0
		for _, state := range states {
			if state.State == nkn.ConnConnected {
				connectedCount++
			}
		}
		log.Printf("Connected clients: %d/%d\n", connectedCount, len(states))

		if connectedCount == 0 {
			log.Println("WARNING: No clients are connected! Message sending may fail.")
		}

		// ============================================
		// Step 4: Send message using cross send policy
		// ============================================
		log.Println("\n=== Step 4: Sending Message with Cross Send Policy ===")
		log.Printf("Sending message from %s to %s", fromClient.Address(), toClient.Address())
		log.Printf("Using CrossSendPolicy: %s", getPolicyString(config.CrossSendPolicy))

		onReply, err := fromClient.Send(nkn.NewStringArray(toClient.Address()), []byte("Hello"), nil)
		if err != nil {
			log.Printf("Error sending message: %v", err)
			log.Println("This may be due to network issues or no connected clients.")
			// Continue to show states even if send fails
		} else {
			// Wait for reply with timeout
			select {
			case reply := <-onReply.C:
				isEncryptedStr := "unencrypted"
				if reply.Encrypted {
					isEncryptedStr = "encrypted"
				}
				timeResponse := time.Now().UnixNano() / int64(time.Millisecond)
				log.Printf("Got %s reply \"%s\" from %s (latency: %d ms)",
					isEncryptedStr, string(reply.Data), reply.Src, timeResponse-timeReceived)
			case <-time.After(15 * time.Second):
				log.Println("Timeout waiting for reply (this may be due to network issues)")
			}
		}

		// Test CrossSendPolicyAllConnected
		log.Println("\n1. Testing CrossSendPolicyAllConnected:")
		broadcastConfig := &nkn.ClientConfig{
			MultiClientOriginalClient: true,
			MultiClientNumClients:     4, // Reduced to avoid connection issues
			CrossSendPolicy:           nkn.CrossSendPolicyAllConnected,
		}
		broadcastClient, err := nkn.NewMultiClientV2(account, hex.EncodeToString(fromIdentifier)+"_broadcast", broadcastConfig)
		if err != nil {
			log.Printf("Error creating broadcast client: %v", err)
			log.Println("Skipping broadcast test due to connection error")
		} else {
			defer broadcastClient.Close()
			// Wait for connection with timeout
			select {
			case <-broadcastClient.OnConnect.C:
				log.Println("Broadcast client connected")
				time.Sleep(time.Second)
				onReply2, err := broadcastClient.Send(nkn.NewStringArray(toClient.Address()), []byte("Broadcast"), nil)
				if err != nil {
					log.Printf("Error sending: %v", err)
				} else {
					select {
					case reply2 := <-onReply2.C:
						log.Printf("Received reply: \"%s\"", string(reply2.Data))
					case <-time.After(10 * time.Second):
						log.Println("Timeout waiting for reply")
					}
				}
			case <-time.After(30 * time.Second):
				log.Println("Timeout waiting for broadcast client connection")
			}
		}

		// Test CrossSendPolicyPreferStable
		log.Println("\n2. Testing CrossSendPolicyPreferStable:")
		stableConfig := &nkn.ClientConfig{
			MultiClientOriginalClient: true,
			MultiClientNumClients:     4, // Reduced to avoid connection issues
			CrossSendPolicy:           nkn.CrossSendPolicyPreferStable,
		}
		stableClient, err := nkn.NewMultiClientV2(account, hex.EncodeToString(fromIdentifier)+"_stable", stableConfig)
		if err != nil {
			log.Printf("Error creating stable client: %v", err)
			log.Println("Skipping stable test due to connection error")
		} else {
			defer stableClient.Close()
			// Wait for connection with timeout
			select {
			case <-stableClient.OnConnect.C:
				log.Println("Stable client connected")
				time.Sleep(time.Second)
				onReply3, err := stableClient.Send(nkn.NewStringArray(toClient.Address()), []byte("Stable"), nil)
				if err != nil {
					log.Printf("Error sending: %v", err)
				} else {
					select {
					case reply3 := <-onReply3.C:
						log.Printf("Received reply: \"%s\"", string(reply3.Data))
					case <-time.After(10 * time.Second):
						log.Println("Timeout waiting for reply")
					}
				}
			case <-time.After(30 * time.Second):
				log.Println("Timeout waiting for stable client connection")
			}
		}

		// ============================================
		// Step 6: Display final states
		// ============================================
		log.Println("\n=== Step 6: Final Sub-Connection States ===")
		finalStates := fromClient.GetAllSubConnStates()
		for _, state := range finalStates {
			stateStr := getStateString(state.State)
			log.Printf("Client[%d]: State=%s", state.Index, stateStr)
			if state.NodeAddr != "" {
				log.Printf("  NodeAddr: %s, NodeID: %s", state.NodeAddr, state.NodeID)
			}
		}

		// Wait to send receipt
		time.Sleep(time.Second)

		return nil
	}()
	if err != nil {
		fmt.Println(err)
	}
}

func getStateString(state nkn.ConnState) string {
	switch state {
	case nkn.ConnConnecting:
		return "Connecting"
	case nkn.ConnConnected:
		return "Connected"
	case nkn.ConnDisconnected:
		return "Disconnected"
	default:
		return "Unknown"
	}
}

func getPolicyString(policy nkn.CrossSendPolicy) string {
	switch policy {
	case nkn.CrossSendPolicyNone:
		return "Default (No cross send)"
	case nkn.CrossSendPolicyAnyConnected:
		return "AnyConnected (Any connected line sends)"
	case nkn.CrossSendPolicyAllConnected:
		return "BroadcastAllConnected (Broadcast all connected lines)"
	case nkn.CrossSendPolicyPreferStable:
		return "PreferStable (Select most stable line)"
	default:
		return "Unknown"
	}
}
