package lamport

import (
	"sync"
)

type Clock struct {
	mu   sync.Mutex
	time int64
}

func NewClock() *Clock {
	return &Clock{
		time: 0,
	}
}

func (c *Clock) Increment() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.time++
	return c.time
}

func (c *Clock) Update(receivedTime int64) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if receivedTime > c.time {
		c.time = receivedTime
	}
	c.time++
	return c.time
}

func (c *Clock) Time() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.time
}
