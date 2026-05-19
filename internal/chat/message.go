package chat

import (
	"time"

	"github.com/google/uuid"
)

// MessageType определяет тип сообщения
type MessageType string

const (
	TypePrivate MessageType = "private" // личное сообщение
	TypeGroup   MessageType = "group"   // групповое
	TypeSystem  MessageType = "system"  // системное
	TypeJoin    MessageType = "join"    // присоединение к группе
	TypeLeave   MessageType = "leave"   // выход из группы
)

// Message структура сообщения
type Message struct {
	ID           uuid.UUID   `json:"id"`
	Type         MessageType `json:"type"`
	FromUserID   uuid.UUID   `json:"from_user_id"`
	FromUsername string      `json:"from_username"`
	ToChatID     string      `json:"to_chat_id"`
	Content      string      `json:"content"`
	ReplyToID    *uuid.UUID  `json:"reply_to_id,omitempty"`
	CreatedAt    time.Time   `json:"created_at"`
}

// NewMessage создаёт новое сообщение
func NewMessage(msgType MessageType, fromUserID uuid.UUID, fromUsername, toChatID, content string) *Message {
	return &Message{
		ID:           uuid.New(),
		Type:         msgType,
		FromUserID:   fromUserID,
		FromUsername: fromUsername,
		ToChatID:     toChatID,
		Content:      content,
		CreatedAt:    time.Now(),
	}
}

// IsPrivateChat проверяет, является ли чат личным
func IsPrivateChat(chatID string) bool {
	return len(chatID) > 5 && chatID[:5] == "user:"
}

// IsGroupChat проверяет, является ли чат групповым
func IsGroupChat(chatID string) bool {
	return len(chatID) > 6 && chatID[:6] == "group:"
}
