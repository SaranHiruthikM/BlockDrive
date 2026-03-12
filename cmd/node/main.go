package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/SaranHiruthik/BlockDrive/internal/node"
)

func main() {
	var (
		id         string
		addr       string
		peersList  string
		storageDir string
		chainPath  string
	)

	flag.StringVar(&id, "id", "node1", "Node ID")
	flag.StringVar(&addr, "addr", ":8080", "Address to listen on")
	flag.StringVar(&peersList, "peers", "", "Comma-separated list of peer addresses")
	flag.StringVar(&storageDir, "storage", "./data/storage", "Directory for file chunks")
	flag.StringVar(&chainPath, "chain", "./data/blockchain.json", "Path for blockchain storage")
	flag.Parse()

	peers := strings.Split(peersList, ",")
	// clean up empty strings if no peers
	var validPeers []string
	for _, p := range peers {
		if p != "" {
			validPeers = append(validPeers, p)
		}
	}

	fmt.Printf("Starting Node %s at %s\n", id, addr)
	fmt.Printf("Peers: %v\n", validPeers)

	n := node.NewNode(id, addr, validPeers, storageDir, chainPath)
	n.Start()

	// Keep main goroutine alive
	select {}
}
