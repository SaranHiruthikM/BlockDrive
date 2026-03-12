package node

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"sync"
	"time"

	"github.com/SaranHiruthik/BlockDrive/internal/blockchain"
	"github.com/SaranHiruthik/BlockDrive/internal/lamport"
	"github.com/SaranHiruthik/BlockDrive/internal/storage"
	"github.com/SaranHiruthik/BlockDrive/pkg/models"
)

type NodeState string

const (
	Follower  NodeState = "FOLLOWER"
	Candidate NodeState = "CANDIDATE"
	Leader    NodeState = "LEADER"
)

type Node struct {
	ID       string
	Address  string
	Peers    []string // Addresses of other nodes
	State    NodeState
	LeaderID string

	Store *storage.Store
	Chain *blockchain.Chain
	Clock *lamport.Clock

	LastHeartbeat time.Time

	mu       sync.Mutex
	stopChan chan struct{}
}

func NewNode(id, address string, peers []string, storageDir, chainPath string) *Node {
	return &Node{
		ID:            id,
		Address:       address,
		Peers:         peers,
		State:         Follower,
		Store:         storage.NewStore(storageDir),
		Chain:         blockchain.NewChain(chainPath),
		Clock:         lamport.NewClock(),
		LastHeartbeat: time.Now(),
		stopChan:      make(chan struct{}),
	}
}

func (n *Node) Start() {
	// Start HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/upload", n.handleUpload)
	mux.HandleFunc("/replicate", n.handleReplicate)
	mux.HandleFunc("/heartbeat", n.handleHeartbeat)
	mux.HandleFunc("/vote", n.handleVote)
	mux.HandleFunc("/ledger", n.handleLedger)
	mux.HandleFunc("/status", n.handleStatus)
	mux.HandleFunc("/download", n.handleDownload)
	mux.HandleFunc("/chunk", n.handleGetChunk) // New handler for peer-to-peer chunk retrieval

	server := &http.Server{
		Addr:    n.Address,
		Handler: mux,
	}

	go func() {
		fmt.Printf("Node %s listening on %s\n", n.ID, n.Address)
		if err := server.ListenAndServe(); err != nil {
			fmt.Printf("Error starting server: %v\n", err)
		}
	}()

	// Start election/heartbeat loop
	go n.runElectionLoop()
}

// Loop to manage leader election and heartbeats
func (n *Node) runElectionLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			n.checkLeaderStatus()
		case <-n.stopChan:
			return
		}
	}
}

func (n *Node) checkLeaderStatus() {
	// Check if heartbeat timeout (if follower) or send heartbeat (if leader)
	n.mu.Lock()
	state := n.State
	lastHeartbeat := n.LastHeartbeat
	n.mu.Unlock()

	if state == Leader {
		for _, peer := range n.Peers {
			go n.sendHeartbeat(peer)
		}
	} else {
		// If heartbeat expired (e.g., > 6s), start election
		if time.Since(lastHeartbeat) > 6*time.Second {
			// Only start election if not already candidate
			n.mu.Lock()
			isCandidate := n.State == Candidate
			n.mu.Unlock()

			if !isCandidate {
				go n.startElection()
			}
		}
	}
}

func (n *Node) sendHeartbeat(peerAddr string) {
	fmt.Printf("Sending heartbeat to %s\n", peerAddr)
	// Fire and forget
	client := http.Client{Timeout: 1 * time.Second}
	_, err := client.Post("http://"+peerAddr+"/heartbeat", "application/json", nil)
	if err != nil {
		fmt.Printf("Failed to heartbeat %s: %v\n", peerAddr, err)
	}
}

// APIs
func (n *Node) handleUpload(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Received upload request")
	// Parse max 10MB
	r.ParseMultipartForm(10 << 20)

	file, handler, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Error retrieving file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	fmt.Printf("File: %s, Size: %d, MIME: %v\n", handler.Filename, handler.Size, handler.Header)

	// Create file metadata
	fileID := fmt.Sprintf("%x", time.Now().UnixNano()) // Simple ID

	// Read file content
	fileBytes, err := ioutil.ReadAll(file)
	if err != nil {
		http.Error(w, "Error reading file", http.StatusInternalServerError)
		return
	}

	// Encrypt (Simple XOR for demo)
	encrypted := simpleEncrypt(fileBytes, "secret_key")

	// Split into chunks (e.g., 1MB each)
	// For demo with small files, let's say 1KB chunks
	const chunkSize = 1024
	numChunks := (len(encrypted) + chunkSize - 1) / chunkSize

	chunkLocs := make(map[int][]string)

	for i := 0; i < numChunks; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > len(encrypted) {
			end = len(encrypted)
		}
		chunkData := encrypted[start:end]

		// Save locally first (primary replica)
		err := n.Store.SaveChunk(fileID, i, chunkData)
		if err != nil {
			fmt.Printf("Error saving chunk %d: %v\n", i, err)
			continue
		}
		chunkLocs[i] = append(chunkLocs[i], n.ID)

		// Replicate to others (Round Robin or Random)
		// For demo: replicate to ALL peers
		for _, peer := range n.Peers {
			go n.replicateChunk(peer, fileID, i, chunkData)
			chunkLocs[i] = append(chunkLocs[i], peer)
		}
	}

	// Record in Blockchain
	op := models.Operation{
		Type:      "UPLOAD",
		Data:      models.FileMetadata{ID: fileID, Name: handler.Filename, Size: handler.Size, Chunks: numChunks, Locations: chunkLocs, Timestamp: time.Now().Unix()},
		Timestamp: time.Now().Unix(),
		NodeID:    n.ID,
	}

	// If Leader, add block. If not, forward to Leader?
	// For simplicity, just add to local chain and broadcast block (simplest consensus)
	// Or better: forward op to owner/leader.

	if n.State == Leader {
		n.Chain.AddBlock([]models.Operation{op})
		// Broadcast new block to peers?
		// Note: Real system uses consensus here (Raft/Paxos). To keep it simple:
		// Leader appends, then periodic sync or broadcast.
	} else {
		// Forward to leader (not implemented yet, just log)
		fmt.Printf("I am not leader. Should forward to %s\n", n.LeaderID)
		// Fallback: Add to local chain anyway for demo purpose so client sees it
		n.Chain.AddBlock([]models.Operation{op})
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf("File %s uploaded successfully. ID: %s", handler.Filename, fileID)))
}

func simpleEncrypt(data []byte, key string) []byte {
	out := make([]byte, len(data))
	for i := 0; i < len(data); i++ {
		out[i] = data[i] ^ key[i%len(key)]
	}
	return out
}

func (n *Node) replicateChunk(peerAddr, fileID string, index int, data []byte) {
	// Simple JSON payload
	payload := map[string]interface{}{
		"fileID": fileID,
		"index":  index,
		"data":   data, // base64 encoded automatically by json.Marshal for []byte
	}
	body, _ := json.Marshal(payload)

	resp, err := http.Post("http://"+peerAddr+"/replicate", "application/json", bytes.NewBuffer(body))
	if err != nil {
		fmt.Printf("Failed to replicate to %s: %v\n", peerAddr, err)
		return
	}
	defer resp.Body.Close()
}

func (n *Node) handleReplicate(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		FileID string `json:"fileID"`
		Index  int    `json:"index"`
		Data   []byte `json:"data"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	fmt.Printf("Received replication chunk %d for file %s\n", payload.Index, payload.FileID)
	// Save to storage
	if err := n.Store.SaveChunk(payload.FileID, payload.Index, payload.Data); err != nil {
		http.Error(w, "Failed to save", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (n *Node) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Received heartbeat")
	n.mu.Lock()
	n.LastHeartbeat = time.Now()
	// Optionally update leader ID from payload if provided
	n.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (n *Node) handleVote(w http.ResponseWriter, r *http.Request) {
	n.handleElectionMessage(w, r)
}

func (n *Node) handleLedger(w http.ResponseWriter, r *http.Request) {
	n.mu.Lock()
	defer n.mu.Unlock()
	bytes, _ := json.Marshal(n.Chain.Blocks)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write(bytes)
}

// handleDownload retrieves a file by ID
func (n *Node) handleDownload(w http.ResponseWriter, r *http.Request) {
	fileID := r.URL.Query().Get("id")
	if fileID == "" {
		http.Error(w, "Missing file ID", http.StatusBadRequest)
		return
	}

	// 1. Find metadata from blockchain
	var meta models.FileMetadata
	found := false

	n.mu.Lock()
	blocks := n.Chain.Blocks
	n.mu.Unlock()

	for i := len(blocks) - 1; i >= 0; i-- { // Reverse search
		for _, op := range blocks[i].Operations {
			if op.Type == "UPLOAD" {
				// Hacky JSON conversion
				dataBytes, _ := json.Marshal(op.Data)
				var m models.FileMetadata
				if err := json.Unmarshal(dataBytes, &m); err == nil && m.ID == fileID {
					meta = m
					found = true
					break
				}
			}
		}
		if found {
			break
		}
	}

	if !found {
		http.Error(w, "File not found in ledger", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", meta.Name))
	w.Header().Set("Content-Type", "application/octet-stream")

	// 2. Retrieve chunks
	for i := 0; i < meta.Chunks; i++ {
		var chunkData []byte
		var err error

		// Try local first
		if n.Store.HasChunk(fileID, i) {
			chunkData, err = n.Store.GetChunk(fileID, i)
		}

		if chunkData == nil {
			// Try fetching from peers
			chunkData, err = n.fetchChunkFromPeers(fileID, i, n.Peers)
		}

		if err != nil || chunkData == nil {
			http.Error(w, fmt.Sprintf("Error retrieving chunk %d", i), http.StatusServiceUnavailable)
			return
		}

		// Decrypt and write to response
		decrypted := simpleEncrypt(chunkData, "secret_key")
		w.Write(decrypted)
	}
}

func (n *Node) fetchChunkFromPeers(fileID string, index int, peers []string) ([]byte, error) {
	for _, peer := range peers {
		url := fmt.Sprintf("http://%s/chunk?id=%s&index=%d", peer, fileID, index)
		resp, err := http.Get(url)
		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			return ioutil.ReadAll(resp.Body)
		}
	}
	return nil, fmt.Errorf("chunk not found")
}

func (n *Node) handleGetChunk(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	fileID := query.Get("id")
	indexStr := query.Get("index")

	var index int
	if _, err := fmt.Sscanf(indexStr, "%d", &index); err != nil {
		http.Error(w, "Invalid index", http.StatusBadRequest)
		return
	}

	if n.Store.HasChunk(fileID, index) {
		data, _ := n.Store.GetChunk(fileID, index)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(data)
	} else {
		http.Error(w, "Not found", http.StatusNotFound)
	}
}

func (n *Node) handleStatus(w http.ResponseWriter, r *http.Request) {
	n.mu.Lock()
	defer n.mu.Unlock()

	status := map[string]interface{}{
		"id":             n.ID,
		"state":          n.State,
		"leader_id":      n.LeaderID,
		"peers":          n.Peers,
		"last_heartbeat": n.LastHeartbeat.Unix(),
		"block_height":   len(n.Chain.Blocks),
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(status)
}
