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

// MutexState represents the state of the node in Ricart-Agrawala algorithm
type MutexState string

const (
	Released MutexState = "RELEASED"
	Wanted   MutexState = "WANTED"
	Held     MutexState = "HELD"
)

type Node struct {
	ID          string
	Address     string   // The public address other nodes should use to contact this node
	BindAddress string   // The local address to bind the HTTP server to (e.g., :8080)
	Peers       []string // Addresses of other nodes
	State       NodeState
	LeaderID    string

	Store *storage.Store
	Chain *blockchain.Chain
	Clock *lamport.Clock

	LastHeartbeat time.Time

	// Ricart-Agrawala Mutex State
	MutexState     MutexState
	MutexTimestamp int64
	MutexReplies   int
	MutexDeferred  []string // List of addresses (deferred replies)

	mu       sync.Mutex
	stopChan chan struct{}
}

func NewNode(id, address, bindAddress string, peers []string, storageDir, chainPath string) *Node {
	return &Node{
		ID:            id,
		Address:       address,
		BindAddress:   bindAddress,
		Peers:         peers,
		State:         Follower,
		Store:         storage.NewStore(storageDir),
		Chain:         blockchain.NewChain(chainPath),
		Clock:         lamport.NewClock(),
		LastHeartbeat: time.Now(),
		MutexState:    Released,
		MutexDeferred: make([]string, 0),
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
	mux.HandleFunc("/chunk", n.handleGetChunk)     // New handler for peer-to-peer chunk retrieval
	mux.HandleFunc("/block/new", n.handleNewBlock) // Add handler for new block synchronization
	mux.HandleFunc("/", n.handleIndex)             // Simple UI for the node

	// Ricart-Agrawala Mutex Handlers
	mux.HandleFunc("/backup/start", n.handleBackupStart)
	mux.HandleFunc("/mutex/request", n.handleMutexRequest)
	mux.HandleFunc("/mutex/reply", n.handleMutexReply)

	server := &http.Server{
		Addr:    n.BindAddress,
		Handler: mux,
	}

	go func() {
		fmt.Printf("Node %s listening on %s (advertised as %s)\n", n.ID, n.BindAddress, n.Address)
		if err := server.ListenAndServe(); err != nil {
			fmt.Printf("Error starting server: %v\n", err)
		}
	}()

	// Initial Sync: Fetch ledger from peers if we are behind
	go n.syncLedger()

	// Start election/heartbeat loop
	go n.runElectionLoop()
}

// Initial Ledger Synchronization
func (n *Node) syncLedger() {
	time.Sleep(3 * time.Second) // Wait for other peers to be available

	n.mu.Lock()
	peers := n.Peers
	n.mu.Unlock()

	for _, peer := range peers {
		resp, err := http.Get("http://" + peer + "/ledger")
		if err != nil {
			fmt.Printf(" [Sync] Failed to contact %s: %v\n", peer, err)
			continue
		}
		defer resp.Body.Close()

		var newChain []models.Block
		if err := json.NewDecoder(resp.Body).Decode(&newChain); err != nil {
			fmt.Printf(" [Sync] Failed to decode ledger from %s: %v\n", peer, err)
			continue
		}

		n.mu.Lock()
		if len(newChain) > len(n.Chain.Blocks) {
			fmt.Printf(" [Sync] Updating local chain (Height: %d) from peer %s (Height: %d)\n", len(n.Chain.Blocks), peer, len(newChain))
			n.Chain.Blocks = newChain
			n.Chain.Save()
		}
		n.mu.Unlock()
	}
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

	// Split into chunks (e.g., 1MB each)
	// For demo with small files, let's say 1KB chunks
	const chunkSize = 1024
	numChunks := (len(fileBytes) + chunkSize - 1) / chunkSize

	chunkLocs := make(map[string][]string)

	for i := 0; i < numChunks; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > len(fileBytes) {
			end = len(fileBytes)
		}

		// Fix: Encrypt EACH chunk independently so they can be decrypted independently
		chunkDataRaw := fileBytes[start:end]
		chunkData := simpleEncrypt(chunkDataRaw, "secret_key")

		// Save locally first (primary replica)
		err := n.Store.SaveChunk(fileID, i, chunkData)
		if err != nil {
			fmt.Printf("Error saving chunk %d: %v\n", i, err)
			continue
		}
		// Use Address instead of ID so other nodes know how to reach us directly
		idxStr := fmt.Sprintf("%d", i)
		chunkLocs[idxStr] = append(chunkLocs[idxStr], n.Address)

		// Sharding V1: Round-Robin Distribution
		// Instead of replicating to ALL peers, we distribute chunks across peers.
		if len(n.Peers) > 0 {
			peerIndex := i % len(n.Peers)
			targetPeer := n.Peers[peerIndex]

			// Replicate to the selected shard/peer
			go n.replicateChunk(targetPeer, fileID, i, chunkData)
			chunkLocs[idxStr] = append(chunkLocs[idxStr], targetPeer)

			fmt.Printf(" [Sharding] Chunk %d assigned to %s\n", i, targetPeer)
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
		newBlock := n.Chain.AddBlock([]models.Operation{op})
		// CRITICAL FIX: Leader MUST broadcast the new block to all followers!
		go n.broadcastBlock(newBlock)
		// Broadcast new block to peers?
		// Note: Real system uses consensus here (Raft/Paxos). To keep it simple:
		// Leader appends, then periodic sync or broadcast.
	} else {
		// Forward to leader (not implemented yet, just log)
		fmt.Printf("I am not leader. Should forward to %s\n", n.LeaderID)
		// Fallback: Add to local chain anyway for demo purpose so client sees it
		newBlock := n.Chain.AddBlock([]models.Operation{op})
		// Broadcast the new block to all peers to ensure ledger consistency
		go n.broadcastBlock(newBlock)
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf("File %s uploaded successfully. ID: %s", handler.Filename, fileID)))
}

func (n *Node) broadcastBlock(block models.Block) {
	data, _ := json.Marshal(block)
	for _, peer := range n.Peers {
		go func(addr string) {
			url := fmt.Sprintf("http://%s/block/new", addr)
			resp, err := http.Post(url, "application/json", bytes.NewBuffer(data))
			if err != nil {
				fmt.Printf("Failed to broadcast block to %s: %v\n", addr, err)
			} else {
				defer resp.Body.Close()
			}
		}(peer)
	}
}

func (n *Node) handleNewBlock(w http.ResponseWriter, r *http.Request) {
	var block models.Block
	if err := json.NewDecoder(r.Body).Decode(&block); err != nil {
		http.Error(w, "Invalid block", http.StatusBadRequest)
		return
	}

	n.mu.Lock()
	// Simply append if index is higher
	// In a real system, we'd validate hash, previous hash, PoW, etc.
	lastBlock := n.Chain.Blocks[len(n.Chain.Blocks)-1]
	if block.Index > lastBlock.Index {
		// Append locally
		n.Chain.Blocks = append(n.Chain.Blocks, block)
		n.Chain.Save() // Persist to disk
		fmt.Printf(" [Ledger] Synced new block %d from peer\n", block.Index)
	}
	n.mu.Unlock()

	w.WriteHeader(http.StatusOK)
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

// handleIndex serves a simple HTML page listing files in the ledger
func (n *Node) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	n.mu.Lock()
	blocks := n.Chain.Blocks
	n.mu.Unlock()

	// Extract unique files
	type FileInfo struct {
		ID   string
		Name string
		Size int64
	}
	files := make(map[string]FileInfo)

	for _, block := range blocks {
		for _, op := range block.Operations {
			if op.Type == "UPLOAD" {
				// Hacky JSON conversion
				dataBytes, _ := json.Marshal(op.Data)
				var m models.FileMetadata
				if err := json.Unmarshal(dataBytes, &m); err == nil {
					files[m.ID] = FileInfo{ID: m.ID, Name: m.Name, Size: m.Size}
				}
			}
		}
	}

	// Simple HTML template
	html := `<!DOCTYPE html>
<html>
<head>
	<title>BlockDrive Node ` + n.ID + `</title>
	<style>
		body { font-family: sans-serif; max-width: 800px; margin: 2rem auto; padding: 0 1rem; }
		table { width: 100%; border-collapse: collapse; margin-top: 1rem; }
		th, td { padding: 0.5rem; text-align: left; border-bottom: 1px solid #ddd; }
		th { background: #f4f4f4; }
		.btn { display: inline-block; padding: 0.5rem 1rem; background: #007bff; color: white; text-decoration: none; border-radius: 4px; }
		.btn:hover { background: #0056b3; }
	</style>
</head>
<body>
	<h1>BlockDrive Node: ` + n.ID + `</h1>
	<p>Address: ` + n.Address + `</p>
	<p>Peers: ` + fmt.Sprintf("%v", n.Peers) + `</p>
	
	<h2>Start Tasks</h2>
	<p><a href="/backup/start" target="_blank" class="btn" style="background-color: #d63384;">Start Cluster Backup (Requires Lock)</a></p>

	<h2>Available Files (from Ledger)</h2>
	<table>
		<tr>
			<th>File Name</th>
			<th>Size (bytes)</th>
			<th>ID</th>
			<th>Action</th>
		</tr>`

	for _, f := range files {
		html += fmt.Sprintf(`
		<tr>
			<td>%s</td>
			<td>%d</td>
			<td>%s</td>
			<td><a href="/download?id=%s" class="btn">Download</a></td>
		</tr>`, f.Name, f.Size, f.ID, f.ID)
	}

	html += `
	</table>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
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
	w.Header().Set("Content-Length", fmt.Sprintf("%d", meta.Size))

	// 2. Retrieve chunks
	for i := 0; i < meta.Chunks; i++ {
		var chunkData []byte
		var err error

		// Try local first
		if n.Store.HasChunk(fileID, i) {
			chunkData, err = n.Store.GetChunk(fileID, i)
		}

		if chunkData == nil {
			// Smart Retrieval: Use the blockchain metadata to find EXACTLY who has the chunk
			// This works even if the uploader is offline or not a direct peer!
			idxStr := fmt.Sprintf("%d", i)
			potentialSources := meta.Locations[idxStr]

			chunkData, err = n.fetchChunkFromPeers(fileID, i, potentialSources)
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
	client := http.Client{Timeout: 5 * time.Second}
	for _, peer := range peers {
		url := fmt.Sprintf("http://%s/chunk?id=%s&index=%d", peer, fileID, index)
		resp, err := client.Get(url)
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

// --- Ricart-Agrawala Mutual Exclusion ---

// Justification:
// We use Ricart-Agrawala to implement a "Distributed Cluster Backup".
// Taking a consistent snapshot of the distributed ledger and storage requires
// exclusive access to ensure no concurrent modifications corrupt the snapshot state.
// Only ONE node is allowed to perform the backup process at a time.

func (n *Node) handleBackupStart(w http.ResponseWriter, r *http.Request) {
	n.mu.Lock()
	if n.MutexState != Released {
		n.mu.Unlock()
		http.Error(w, "Cluster backup already in progress or queued", http.StatusConflict)
		return
	}

	n.MutexState = Wanted
	n.MutexTimestamp = n.Clock.Increment()
	n.MutexReplies = 0
	n.MutexDeferred = make([]string, 0)
	peers := n.Peers
	// Check peer count
	requiredReplies := len(peers)
	n.mu.Unlock()

	fmt.Printf("[Mutex] Requesting Global Lock for Backup. Timestamp: %d. Peers: %d\n", n.MutexTimestamp, requiredReplies)

	if requiredReplies == 0 {
		n.enterCriticalSection()
		w.Write([]byte("Entered critical section immediately (no peers)"))
		return
	}

	// Broadcast REQUEST
	for _, peer := range peers {
		go n.sendMutexRequest(peer)
	}

	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte("Requesting global lock for backup..."))
}

func (n *Node) sendMutexRequest(peer string) {
	n.mu.Lock()
	ts := n.MutexTimestamp
	myID := n.ID
	myAddr := n.Address
	n.mu.Unlock()

	url := fmt.Sprintf("http://%s/mutex/request?ts=%d&id=%s&addr=%s", peer, ts, myID, myAddr)
	http.Post(url, "application/json", nil)
}

func (n *Node) handleMutexRequest(w http.ResponseWriter, r *http.Request) {
	tsStr := r.URL.Query().Get("ts")
	reqID := r.URL.Query().Get("id")
	reqAddr := r.URL.Query().Get("addr")
	var reqTs int64
	fmt.Sscanf(tsStr, "%d", &reqTs)

	n.Clock.Update(reqTs)

	n.mu.Lock()
	deferRequest := false

	if n.MutexState == Held {
		deferRequest = true
	} else if n.MutexState == Wanted {
		// Tie-breaking: if my TS is lower (older), I win. If equal, strictly order by ID.
		if n.MutexTimestamp < reqTs || (n.MutexTimestamp == reqTs && n.ID < reqID) {
			deferRequest = true
		}
	}

	if deferRequest {
		fmt.Printf("[Mutex] Deferring request from %s (MyTS: %d, ReqTS: %d)\n", reqID, n.MutexTimestamp, reqTs)
		n.MutexDeferred = append(n.MutexDeferred, reqAddr)
		n.mu.Unlock()
	} else {
		n.mu.Unlock()
		fmt.Printf("[Mutex] Granting permission to %s\n", reqID)
		go n.sendMutexReply(reqAddr)
	}

	w.WriteHeader(http.StatusOK)
}

func (n *Node) sendMutexReply(peerAddr string) {
	url := fmt.Sprintf("http://%s/mutex/reply?from=%s", peerAddr, n.ID)
	http.Post(url, "application/json", nil)
}

func (n *Node) handleMutexReply(w http.ResponseWriter, r *http.Request) {
	n.mu.Lock()
	n.MutexReplies++
	replies := n.MutexReplies
	required := len(n.Peers)
	state := n.MutexState
	n.mu.Unlock()

	fmt.Printf("[Mutex] Received reply. Total: %d/%d\n", replies, required)

	if state == Wanted && replies >= required {
		n.enterCriticalSection()
	}
	w.WriteHeader(http.StatusOK)
}

func (n *Node) enterCriticalSection() {
	n.mu.Lock()
	n.MutexState = Held
	n.mu.Unlock()

	fmt.Println("!!! ENTERING CRITICAL SECTION (Global Lock Acquired) !!!")
	fmt.Println(" Performing Cluster-Wide Backup Snapshot...")
	fmt.Println(" [LOCK] All other write operations are logically suspended.")

	// Simulate heavy backup work
	go func() {
		time.Sleep(5 * time.Second) // Simulating backup time

		// Add "BACKUP_COMPLETE" Record to Chain
		op := models.Operation{
			Type:      "SYSTEM_BACKUP",
			Data:      "Snapshot ID: " + fmt.Sprintf("%x", time.Now().Unix()),
			Timestamp: time.Now().Unix(),
			NodeID:    n.ID,
		}

		n.mu.Lock()
		newBlock := n.Chain.AddBlock([]models.Operation{op})
		n.mu.Unlock()

		go n.broadcastBlock(newBlock)

		fmt.Println(" Backup completed. Log added to blockchain.")

		n.exitCriticalSection()
	}()
}

func (n *Node) exitCriticalSection() {
	fmt.Println("!!! EXITING CRITICAL SECTION (Global Lock Released) !!!")

	n.mu.Lock()
	n.MutexState = Released
	deferred := n.MutexDeferred
	n.MutexDeferred = make([]string, 0)
	n.mu.Unlock()

	for _, addr := range deferred {
		fmt.Printf("[Mutex] Sending deferred reply to %s\n", addr)
		go n.sendMutexReply(addr)
	}
}
