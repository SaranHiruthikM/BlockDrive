package blockchain

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"sync"
	"time"

	"github.com/SaranHiruthik/BlockDrive/pkg/models"
)

type Chain struct {
	Blocks []models.Block
	mu     sync.Mutex
	path   string
}

func NewChain(path string) *Chain {
	c := &Chain{
		path: path,
	}
	// Try loading from disk
	if err := c.Load(); err != nil {
		c.Blocks = []models.Block{c.createGenesisBlock()}
	}
	return c
}

func (c *Chain) createGenesisBlock() models.Block {
	return models.Block{
		Index:        0,
		Timestamp:    time.Now().Unix(),
		PreviousHash: "0",
		Operations:   []models.Operation{},
		Hash:         "genesis_block_hash",
	}
}

func (c *Chain) AddBlock(ops []models.Operation) models.Block {
	c.mu.Lock()
	defer c.mu.Unlock()

	prevBlock := c.Blocks[len(c.Blocks)-1]
	newBlock := models.Block{
		Index:        prevBlock.Index + 1,
		Timestamp:    time.Now().Unix(),
		PreviousHash: prevBlock.Hash,
		Operations:   ops,
	}
	newBlock.Hash = newBlock.CalculateHash()
	c.Blocks = append(c.Blocks, newBlock)
	c.Save()
	return newBlock
}

func (c *Chain) Validate() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i := 1; i < len(c.Blocks); i++ {
		curr := c.Blocks[i]
		prev := c.Blocks[i-1]

		if curr.Hash != curr.CalculateHash() {
			return false
		}
		if curr.PreviousHash != prev.Hash {
			return false
		}
	}
	return true
}

func (c *Chain) Save() error {
	data, err := json.MarshalIndent(c.Blocks, "", "  ")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(c.path, data, 0644)
}

func (c *Chain) Load() error {
	if _, err := os.Stat(c.path); os.IsNotExist(err) {
		return err
	}
	data, err := ioutil.ReadFile(c.path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &c.Blocks)
}

func (c *Chain) GetLatestBlock() models.Block {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Blocks[len(c.Blocks)-1]
}
