package models

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// MessageType defines the type for network messages
type MessageType string

const (
	MsgHeartbeat    MessageType = "HEARTBEAT"
	MsgElection     MessageType = "ELECTION"
	MsgCoordinator  MessageType = "COORDINATOR"
	MsgProposeBlock MessageType = "PROPOSE_BLOCK"
	MsgVoteBlock    MessageType = "VOTE_BLOCK"
	MsgReplicate    MessageType = "REPLICATE"
	MsgRequestFile  MessageType = "REQUEST_FILE"
	MsgSyncLedger   MessageType = "SYNC_LEDGER"
)

// Message defines the structure for network messages payload
type Message struct {
	Type      MessageType `json:"type"`
	Payload   interface{} `json:"payload"`
	SenderID  string      `json:"sender_id"`
	Timestamp int64       `json:"timestamp"`
}

// Node represents a node in the network
type Node struct {
	ID      string `json:"id"`
	Address string `json:"address"` // IP:Port
}

// FileMetadata stores file information
type FileMetadata struct {
	ID        string              `json:"id"`
	Name      string              `json:"name"`
	Size      int64               `json:"size"`
	Owner     string              `json:"owner"`
	Chunks    int                 `json:"chunks"`
	Locations map[string][]string `json:"locations"` // Map chunk index (as string) to node IDs
	Timestamp int64               `json:"timestamp"`
}

// Block represents a block in the blockchain
type Block struct {
	Index        int         `json:"index"`
	Timestamp    int64       `json:"timestamp"`
	PreviousHash string      `json:"previous_hash"`
	Operations   []Operation `json:"operations"`
	Hash         string      `json:"hash"`
}

// Operation represents a single operation in a block
type Operation struct {
	Type      string      `json:"type"` // UPLOAD, REPLICATE, DELETE, HELLO, JOIN
	Data      interface{} `json:"data"`
	Timestamp int64       `json:"timestamp"`
	NodeID    string      `json:"node_id"`
}

// CalculateHash generates the hash of the block
// simplified for demonstration
func (b *Block) CalculateHash() string {
	record := fmt.Sprintf("%d%d%s%v", b.Index, b.Timestamp, b.PreviousHash, b.Operations)
	h := sha256.New()
	h.Write([]byte(record))
	hashed := h.Sum(nil)
	return hex.EncodeToString(hashed)
}
