package chat

import (
	"time"

	"github.com/google/uuid"
)

// Message - универсальная структура для всех сообщений в системе
type Message struct {
	ID           uuid.UUID  `json:"id"`                    // Уникальный ID сообщения
	Type         string     `json:"type"`                  // Тип сообщения (строка)
	FromUserID   uuid.UUID  `json:"from_user_id"`          // UUID отправителя (из БД)
	FromUsername string     `json:"from_username"`         // Имя отправителя
	ToChatID     string     `json:"to_chat_id"`            // Получатель ("user:name" или "room:name")
	Content      string     `json:"content"`               // Текст сообщения
	ReplyToID    *uuid.UUID `json:"reply_to_id,omitempty"` // ID ответа (для тредов)
	CreatedAt    time.Time  `json:"created_at"`            // Временная метка
}

// NewMessage - конструктор для создания нового сообщения
func NewMessage(msgType string, fromUserID uuid.UUID, fromUsername, toChatID, content string) *Message {
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

// IsPrivateChat - проверяет, является ли chatID идентификатором личного чата
func IsPrivateChat(chatID string) bool {
	// Проверяем длину (минимум "user:x" = 6 символов) и проверяем префикс "user:"
	return len(chatID) > 5 && chatID[:5] == "user:"
}

// IsRoomChat - проверяет, является ли chatID идентификатором комнаты
func IsRoomChat(chatID string) bool {
	// Проверяем длину (минимум "room:x" = 6 символов) и проверяем префикс "room:"
	return len(chatID) > 5 && chatID[:5] == "room:"
}
