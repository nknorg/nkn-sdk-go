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
		// Step 1: Create receiver client
		// ============================================
		log.Println("=== Step 1: Creating Receiver Client ===")
		toClient, err := nkn.NewMultiClientV2(account, hex.EncodeToString(toIdentifier), &nkn.ClientConfig{
			MultiClientOriginalClient: true,
			MultiClientNumClients:     3,
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

		// Set up message receiver
		messageCount := 0
		go func() {
			for {
				msg, ok := <-toClient.OnMessage.C
				if !ok {
					return
				}
				messageCount++
				log.Printf("Receiver: Received message #%d \"%s\" from %s",
					messageCount, string(msg.Data), msg.Src)
				msg.Reply([]byte("OK"))
			}
		}()

		// ============================================
		// Step 2: Create sender client with PreferStable policy
		// ============================================
		log.Println("\n=== Step 2: Creating Sender Client with PreferStable Policy ===")
		config := &nkn.ClientConfig{
			MultiClientOriginalClient: true,
			MultiClientNumClients:     5, // Create 5 sub-clients for better demonstration
			CrossSendPolicy:           nkn.CrossSendPolicyPreferStable,
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
		maxWaitTime := 15 * time.Second
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
			if connectedCount >= 3 || time.Since(startTime) > maxWaitTime {
				log.Printf("Connected clients: %d/%d", connectedCount, len(states))
				break
			}
			time.Sleep(checkInterval)
		}
		time.Sleep(2 * time.Second) // Give clients more time to establish stable connections

		// ============================================
		// Step 3: Display initial connection states and scores
		// ============================================
		log.Println("\n=== Step 3: Initial Connection States and Scores ===")
		displayClientScores(fromClient)

		// ============================================
		// Step 4: Send messages and observe which client is selected
		// ============================================
		log.Println("\n=== Step 4: Sending Messages (Observing Client Selection) ===")
		log.Println("Sending 5 messages to observe which client is selected by the scoring system...")

		for i := 1; i <= 5; i++ {
			log.Printf("\n--- Sending Message #%d ---", i)
			displayClientScores(fromClient)

			onReply, err := fromClient.Send(nkn.NewStringArray(toClient.Address()), []byte(fmt.Sprintf("Message-%d", i)), nil)
			if err != nil {
				log.Printf("Error sending message: %v", err)
				continue
			}

			// Wait for reply with timeout
			select {
			case reply := <-onReply.C:
				log.Printf("✓ Got reply \"%s\" from %s", string(reply.Data), reply.Src)
			case <-time.After(10 * time.Second):
				log.Println("✗ Timeout waiting for reply")
			}

			// Wait a bit between messages to allow connection durations to differ
			time.Sleep(2 * time.Second)
		}

		// ============================================
		// Step 5: Wait and observe how scores change over time
		// ============================================
		log.Println("\n=== Step 5: Observing Score Changes Over Time ===")
		log.Println("Waiting 10 seconds to observe how connection duration affects scores...")
		for i := 0; i < 5; i++ {
			time.Sleep(2 * time.Second)
			log.Printf("\n--- After %d seconds ---", (i+1)*2)
			displayClientScores(fromClient)
		}

		// ============================================
		// Step 6: Final scores
		// ============================================
		log.Println("\n=== Step 6: Final Scores ===")
		displayClientScores(fromClient)

		log.Println("\n=== Demo Complete ===")
		log.Println("The scoring system selects the client with the highest score based on:")
		log.Println("  - Connection duration (longer = higher score)")
		log.Println("  - Reconnect count (fewer = higher score)")
		log.Println("  - Connection state (must be connected)")

		return nil
	}()
	if err != nil {
		fmt.Println(err)
	}
}

// displayClientScores displays connection states and calculated scores for all clients
func displayClientScores(mc *nkn.MultiClient) {
	states := mc.GetAllSubConnStates()
	now := time.Now()

	log.Println("\nClient Scores (sorted by score, highest first):")
	log.Println("┌───────┬────────────┬──────────────┬──────────────┬──────────────┬───────────┬─────────┐")
	log.Println("│ Client│   State    │ Connect Time │ Reconnects   │ Send Failures │ Duration  │  Score  │")
	log.Println("├───────┼────────────┼──────────────┼──────────────┼──────────────┼───────────┼─────────┤")

	type clientScoreInfo struct {
		clientID         int
		state            string
		connectTime      time.Time
		reconnectCount   int
		sendFailureCount int
		duration         time.Duration
		score            float64
	}

	var scoreInfos []clientScoreInfo

	for _, state := range states {
		clientID := state.Index
		stats := mc.GetClientStats(clientID)

		var score float64
		var duration time.Duration
		var connectTime time.Time
		var reconnectCount int
		var sendFailureCount int
		stateStr := getStateString(state.State)

		if state.State == nkn.ConnConnected && stats != nil {
			connectTime = stats.ConnectTime
			reconnectCount = stats.ReconnectCount
			sendFailureCount = stats.SendFailureCount
			duration = now.Sub(connectTime)
			score = calculateScore(duration, reconnectCount, sendFailureCount)
		} else {
			score = 0.0
		}

		scoreInfos = append(scoreInfos, clientScoreInfo{
			clientID:         clientID,
			state:            stateStr,
			connectTime:      connectTime,
			reconnectCount:   reconnectCount,
			sendFailureCount: sendFailureCount,
			duration:         duration,
			score:            score,
		})
	}

	// Sort by score (highest first)
	for i := 0; i < len(scoreInfos)-1; i++ {
		for j := i + 1; j < len(scoreInfos); j++ {
			if scoreInfos[i].score < scoreInfos[j].score {
				scoreInfos[i], scoreInfos[j] = scoreInfos[j], scoreInfos[i]
			}
		}
	}

	// Display sorted results
	for _, info := range scoreInfos {
		var connectTimeStr string
		if !info.connectTime.IsZero() {
			connectTimeStr = formatDuration(now.Sub(info.connectTime))
		} else {
			connectTimeStr = "N/A"
		}

		durationStr := formatDuration(info.duration)
		if info.duration == 0 {
			durationStr = "N/A"
		}

		marker := " "
		if info.state == "Connected" && info.score > 0 {
			// Mark the highest scoring connected client
			if len(scoreInfos) > 0 && info.clientID == scoreInfos[0].clientID {
				marker = "★" // Star indicates likely selected client
			}
		}

		log.Printf("│ %s%3d  │ %-10s │ %-12s │ %-12d │ %-12d │ %-9s │ %6.3f │",
			marker, info.clientID, info.state, connectTimeStr, info.reconnectCount, info.sendFailureCount, durationStr, info.score)
	}

	log.Println("└───────┴────────────┴──────────────┴──────────────┴──────────────┴───────────┴─────────┘")
	log.Println("★ = Highest scoring connected client (likely to be selected)")
}

// calculateScore calculates the same score as MultiClient.CalculateClientScore
// This is a copy for demonstration purposes
func calculateScore(connectionDuration time.Duration, reconnectCount int, sendFailureCount int) float64 {
	// Base score starts at 0.5 for being connected
	score := 0.5

	// Connection duration score (0-0.4 points)
	if connectionDuration > 0 {
		durationScore := float64(connectionDuration) / float64(time.Hour)
		if durationScore > 1.0 {
			durationScore = 1.0
		}
		score += durationScore * 0.4
	}

	// Reconnect penalty (0-0.1 points deduction)
	reconnectPenalty := float64(reconnectCount) * 0.02
	if reconnectPenalty > 0.1 {
		reconnectPenalty = 0.1
	}
	score -= reconnectPenalty

	// Send failure penalty (0-0.1 points deduction)
	sendFailurePenalty := float64(sendFailureCount) * 0.01
	if sendFailurePenalty > 0.1 {
		sendFailurePenalty = 0.1
	}
	score -= sendFailurePenalty

	// Ensure score is between 0 and 1
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}

	return score
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%.1fm", d.Minutes())
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
