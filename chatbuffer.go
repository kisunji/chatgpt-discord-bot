package main

import (
	"log"
	"sync"
	"time"

	"github.com/sashabaranov/go-openai"
)

type ChatBuffer struct {
	sync.Mutex

	msgs []openai.ChatCompletionMessage
	// sizes is index-aligned with msgs
	sizes []int
	// timestamps is index-aligned with msgs
	timestamps []time.Time

	currSize int
	maxSize  int
	maxTime  time.Duration
}

func NewChatBuffer() *ChatBuffer {
	return &ChatBuffer{
		msgs:       []openai.ChatCompletionMessage{},
		sizes:      []int{},
		timestamps: []time.Time{},
		maxSize:    128 * 1024,
		maxTime:    time.Hour,
	}
}

func (c *ChatBuffer) Msgs() []openai.ChatCompletionMessage {
	c.Lock()
	l.RLock() // l globally guards Prompts
	defer l.RUnlock()
	defer c.Unlock()
	return append([]openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleSystem,
			Content: Prompts,
		},
	}, c.msgs...)
}

func (c *ChatBuffer) Add(msg openai.ChatCompletionMessage) {
	c.Lock()
	defer c.Unlock()

	c.flushOld()

	size := len([]byte(msg.Content))
	for c.currSize+size > c.maxSize {
		log.Println("evicting...")
		c.evict()
	}

	c.msgs = append(c.msgs, msg)
	c.currSize += size
	c.sizes = append(c.sizes, size)
	c.timestamps = append(c.timestamps, time.Now())
}

func (c *ChatBuffer) flushOld() {
	for i := len(c.timestamps) - 1; i >= 0; i-- {
		if time.Since(c.timestamps[i]) > c.maxTime {
			log.Println("flushing old timestamps")
			c.msgs = c.msgs[i+1:]
			c.sizes = c.sizes[i+1:]
			c.timestamps = c.timestamps[i+1:]
			newSize := 0
			for _, size := range c.sizes {
				newSize += size
			}
			c.currSize = newSize
			return
		}
	}
}

func (c *ChatBuffer) evict() {
	c.msgs = c.msgs[1:]
	c.currSize -= c.sizes[0]
	c.sizes = c.sizes[1:]
	c.timestamps = c.timestamps[1:]
}
