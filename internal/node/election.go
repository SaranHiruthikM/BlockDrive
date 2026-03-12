package node

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/SaranHiruthik/BlockDrive/pkg/models"
)

// Consts for election timing
const (
	HeartbeatInterval = 2 * time.Second
	ElectionTimeout   = 2 * time.Second
)

func (n *Node) startElection() {
	n.mu.Lock()
	n.State = Candidate
	n.mu.Unlock()

	fmt.Printf("Node %s starting election\n", n.ID)

	higherNodeFound := false
	var wg sync.WaitGroup
	var mu sync.Mutex // to protect higherNodeFound

	for _, peer := range n.Peers {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			isHigher := n.sendElectionMessage(addr)
			if isHigher {
				mu.Lock()
				higherNodeFound = true
				mu.Unlock()
			}
		}(peer)
	}
	wg.Wait()

	if !higherNodeFound {
		// No one higher responded, so I am the leader
		n.becomeLeader()
	} else {
		// Someone higher is alive, so I wait for Coordinator message
		// If timeout, start again?
		// Simplified: just return to Follower and wait.
		// Real impl would have timeout to restart if new leader doesn't announce.
		n.mu.Lock()
		n.State = Follower
		n.mu.Unlock()
	}
}

func (n *Node) becomeLeader() {
	n.mu.Lock()
	n.State = Leader
	n.LeaderID = n.ID
	n.mu.Unlock()

	fmt.Printf("Node %s became LEADER\n", n.ID)

	// Announce victory
	n.broadcastCoordinator()
}

func (n *Node) broadcastCoordinator() {
	msg := models.Message{
		Type:     models.MsgCoordinator,
		Payload:  n.ID, // Payload is leader ID
		SenderID: n.ID,
	}

	body, _ := json.Marshal(msg)
	for _, peer := range n.Peers {
		go func(addr string) {
			http.Post("http://"+addr+"/vote", "application/json", bytes.NewBuffer(body))
		}(peer)
	}
}

// sendElectionMessage returns true if the peer is higher ID and responded
func (n *Node) sendElectionMessage(peerAddr string) bool {
	msg := models.Message{
		Type:     models.MsgElection,
		Payload:  n.ID,
		SenderID: n.ID,
	}
	body, _ := json.Marshal(msg)

	client := http.Client{Timeout: 1 * time.Second}
	resp, err := client.Post("http://"+peerAddr+"/vote", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		// If status OK, it means they are higher and accepted the challenge
		// We expect them to take over.
		return true
	}
	// If 400 or other, they ignored us (lower ID)
	return false
}

func (n *Node) handleElectionMessage(w http.ResponseWriter, r *http.Request) {
	var msg models.Message
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	if msg.Type == models.MsgCoordinator {
		// New leader announced
		fmt.Printf("New Leader: %s\n", msg.SenderID)
		n.mu.Lock()
		n.State = Follower
		n.LeaderID = msg.SenderID
		n.LastHeartbeat = time.Now()
		n.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		return
	}

	if msg.Type == models.MsgElection {
		senderID := msg.Payload.(string)

		// If my ID > sender ID, I respond OK (I am higher)
		// And I start my own election if not already
		if n.ID > senderID {
			w.WriteHeader(http.StatusOK) // "I am higher"

			go func() {
				// Avoid deadlock and infinite recursion
				n.mu.Lock()
				if n.State != Candidate && n.State != Leader {
					n.mu.Unlock()
					n.startElection()
				} else {
					n.mu.Unlock()
				}
			}()
		} else {
			// I am lower, I ignore (or return error code to indicate "you are higher")
			w.WriteHeader(http.StatusTeapot) // 418 I'm a teapot (or just 409 Conflict / 400 Bad Request)
			// Using 400 to indicate "I am not higher than you"
		}
		return
	}
}
