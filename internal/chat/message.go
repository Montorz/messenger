package chat

import (
	"time"

	"github.com/google/uuid"
)

type MessageType string

const (
	TypePrivate MessageType = "private"
	TypeGroup   MessageType = "group"
	TypeSystem  MessageType = "system"
	TypeJoin    MessageType = "join"
	TypeLeave   MessageType = "leave"
	TypeCreate  MessageType = "create_room"
	TypeList    MessageType = "list_rooms"
)

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

func IsPrivateChat(chatID string) bool {
	return len(chatID) > 5 && chatID[:5] == "user:"
}

func IsRoomChat(chatID string) bool {
	return len(chatID) > 5 && chatID[:5] == "room:"
}
