package ai

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Message is a single chat message in the OpenAI-compatible format.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

// ToolCall is a tool invocation requested by the model.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolCallFunc `json:"function"`
}

// ToolCallFunc holds the function name and its JSON-encoded arguments.
type ToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Tool is a function definition exposed to the model.
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction describes a callable function and its JSON schema parameters.
type ToolFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// Reply is the parsed answer of the model: text plus optional tool calls.
type Reply struct {
	Content   string
	ToolCalls []ToolCall
}

// Conversation is a multi-turn chat session. It is kept in memory only.
type Conversation struct {
	ID            string
	Messages      []Message
	Authorization string
	CreatedAt     time.Time
	LastUsed      time.Time
}

// ConversationStore keeps conversations in memory with a TTL.
type ConversationStore struct {
	mu            sync.Mutex
	conversations map[string]*Conversation
	ttl           time.Duration
}

// NewConversationStore returns a store expiring conversations after ttl.
func NewConversationStore(ttl time.Duration) *ConversationStore {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &ConversationStore{
		conversations: make(map[string]*Conversation),
		ttl:           ttl,
	}
}

func newConversationID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte(time.Now().String()))
	}
	return hex.EncodeToString(b)
}

// New creates a new conversation and returns it.
func (s *ConversationStore) New() *Conversation {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	c := &Conversation{
		ID:        newConversationID(),
		CreatedAt: time.Now().UTC(),
		LastUsed:  time.Now().UTC(),
	}
	s.conversations[c.ID] = c
	return c
}

// Get returns the conversation with the given id, or nil if unknown/expired.
// If id is empty, a new conversation is created.
func (s *ConversationStore) Get(id string) *Conversation {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	if id == "" {
		c := &Conversation{
			ID:        newConversationID(),
			CreatedAt: time.Now().UTC(),
			LastUsed:  time.Now().UTC(),
		}
		s.conversations[c.ID] = c
		return c
	}
	c, ok := s.conversations[id]
	if !ok {
		return nil
	}
	c.LastUsed = time.Now().UTC()
	return c
}

func (s *ConversationStore) expireLocked() {
	for id, c := range s.conversations {
		if time.Since(c.LastUsed) > s.ttl {
			delete(s.conversations, id)
		}
	}
}

// Append adds messages to the conversation.
func (c *Conversation) Append(msgs ...Message) {
	c.Messages = append(c.Messages, msgs...)
	c.LastUsed = time.Now().UTC()
}

// SetAuthorization stores the authorization reference on the conversation.
func (c *Conversation) SetAuthorization(ref string) {
	c.Authorization = ref
	c.LastUsed = time.Now().UTC()
}
