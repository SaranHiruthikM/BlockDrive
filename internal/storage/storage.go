package storage

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
)

type Store struct {
	baseDir string
	mu      sync.Mutex
}

func NewStore(baseDir string) *Store {
	os.MkdirAll(baseDir, 0755)
	return &Store{
		baseDir: baseDir,
	}
}

func (s *Store) SaveChunk(fileID string, chunkIndex int, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	filename := fmt.Sprintf("%s_chunk_%d.dat", fileID, chunkIndex)
	path := filepath.Join(s.baseDir, filename)
	return ioutil.WriteFile(path, data, 0644)
}

func (s *Store) GetChunk(fileID string, chunkIndex int) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	filename := fmt.Sprintf("%s_chunk_%d.dat", fileID, chunkIndex)
	path := filepath.Join(s.baseDir, filename)
	return ioutil.ReadFile(path)
}

func (s *Store) DeleteChunk(fileID string, chunkIndex int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	filename := fmt.Sprintf("%s_chunk_%d.dat", fileID, chunkIndex)
	path := filepath.Join(s.baseDir, filename)
	return os.Remove(path)
}

func (s *Store) HasChunk(fileID string, chunkIndex int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	filename := fmt.Sprintf("%s_chunk_%d.dat", fileID, chunkIndex)
	path := filepath.Join(s.baseDir, filename)
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}
