package chat

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/Montorz/messenger/internal/storage"
	"github.com/google/uuid"
)

type Hub struct {
	clients    map[uuid.UUID]*Client
	rooms      map[string]map[uuid.UUID]*Client
	register   chan *Client
	unregister chan *Client
	broadcast  chan *Message
	mu         sync.RWMutex
	db         *storage.DB // добавляем БД
}

// NewHub создаёт новый хаб с БД
func NewHub(db *storage.DB) *Hub {
	return &Hub{
		clients:    make(map[uuid.UUID]*Client),
		rooms:      make(map[string]map[uuid.UUID]*Client),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan *Message, 256),
		db:         db,
	}
}

// sendHistory отправляет историю чата новому клиенту
func (h *Hub) sendHistory(client *Client, chatID string) {
	history, err := h.db.GetChatHistory(chatID, 50)
	if err != nil {
		log.Printf("Ошибка получения истории для %s: %v", chatID, err)
		return
	}

	if len(history) == 0 {
		return
	}

	// Отправляем историю клиенту
	for _, msg := range history {
		historyMsg := &Message{
			ID:           uuid.New(),
			Type:         "system",
			FromUserID:   uuid.MustParse(msg["from_user_id"].(string)),
			FromUsername: msg["from_username"].(string),
			ToChatID:     chatID,
			Content:      msg["content"].(string),
			CreatedAt:    msg["created_at"].(time.Time),
		}
		client.Send <- historyMsg
	}
	log.Printf("Отправлена история (%d сообщений) для чата %s клиенту %s",
		len(history), chatID, client.Username)
}

// Run запускает основной цикл хаба
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.ID] = client
			log.Printf("Клиент %s (%s) подключился", client.Username, client.ID.String()[:8])

			// Автоматически добавляем в общую комнату
			h.joinRoomUnsafe("group:general", client)

			// Отправляем историю общей комнаты
			go h.sendHistory(client, "group:general")
			h.mu.Unlock()

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client.ID]; ok {
				delete(h.clients, client.ID)
				log.Printf("Клиент %s отключился", client.Username)

				// удалить из всех комнат
				for roomID, members := range h.rooms {
					if _, ok := members[client.ID]; ok {
						delete(members, client.ID)
						if len(members) == 0 {
							delete(h.rooms, roomID)
						}
					}
				}
			}
			h.mu.Unlock()
			close(client.Send)

		case msg := <-h.broadcast:
			log.Printf("Получено сообщение: type=%s, to=%s, from=%s, content=%s",
				msg.Type, msg.ToChatID, msg.FromUsername, msg.Content)

			// ОБРАБОТКА СИСТЕМНЫХ КОМАНД
			if msg.Type == "get_users" {
				log.Printf("Обработка /users от %s", msg.FromUsername)

				h.mu.RLock()
				var users []string
				var targetClient *Client
				for _, client := range h.clients {
					users = append(users, client.Username)
					if client.Username == msg.FromUsername {
						targetClient = client
					}
				}
				onlineCount := len(users)
				h.mu.RUnlock()

				if targetClient != nil {
					response := &Message{
						ID:           uuid.New(),
						Type:         "system",
						ToChatID:     "user:" + targetClient.Username,
						Content:      fmt.Sprintf("Онлайн (%d): %s", onlineCount, strings.Join(users, ", ")),
						FromUsername: "system",
						CreatedAt:    time.Now(),
					}
					targetClient.Send <- response
					log.Printf("Список отправлен %s", targetClient.Username)
				}
				continue
			}

			// СОХРАНЯЕМ СООБЩЕНИЕ В БД
			go func(m *Message) {
				var replyID *uuid.UUID
				if m.ReplyToID != nil {
					replyID = m.ReplyToID
				}
				// Правильный порядок аргументов: fromUserID, toChatID, content, replyToID
				if err := h.db.SaveMessage(m.FromUserID, m.ToChatID, m.Content, replyID); err != nil {
					log.Printf("Ошибка сохранения: %v", err)
				}
			}(msg)

			// Обработка групповых сообщений
			if IsGroupChat(msg.ToChatID) {
				h.mu.RLock()
				members, ok := h.rooms[msg.ToChatID]
				h.mu.RUnlock()

				if ok {
					log.Printf("Отправляем в группу %s, участников: %d", msg.ToChatID, len(members))
					for _, client := range members {
						select {
						case client.Send <- msg:
						default:
							log.Printf("Клиент %s недоступен", client.Username)
						}
					}
				}
			} else if IsPrivateChat(msg.ToChatID) {
				// Личное сообщение
				username := msg.ToChatID[5:]

				h.mu.RLock()
				var targetClient *Client
				for _, client := range h.clients {
					if client.Username == username {
						targetClient = client
						break
					}
				}
				h.mu.RUnlock()

				if targetClient != nil {
					targetClient.Send <- msg
					log.Printf("Личное сообщение отправлено %s", username)
				} else {
					log.Printf("Пользователь %s не в сети", username)
					// Отправляем уведомление отправителю
					h.mu.RLock()
					var sender *Client
					for _, client := range h.clients {
						if client.Username == msg.FromUsername {
							sender = client
							break
						}
					}
					h.mu.RUnlock()

					if sender != nil {
						errorMsg := &Message{
							ID:           uuid.New(),
							Type:         "system",
							ToChatID:     "user:" + sender.Username,
							Content:      fmt.Sprintf("Пользователь %s не в сети", username),
							FromUsername: "system",
							CreatedAt:    time.Now(),
						}
						sender.Send <- errorMsg
					}
				}
			}
		}
	}
}

// joinRoomUnsafe добавляет клиента в комнату (без блокировки, используется внутри)
func (h *Hub) joinRoomUnsafe(roomID string, client *Client) {
	if _, ok := h.rooms[roomID]; !ok {
		h.rooms[roomID] = make(map[uuid.UUID]*Client)
		log.Printf("Создана новая комната: %s", roomID)
	}
	h.rooms[roomID][client.ID] = client
	log.Printf("Клиент %s присоединился к комнате %s", client.Username, roomID)
}

// JoinRoom публичный метод для присоединения к комнате
func (h *Hub) JoinRoom(roomID string, client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.joinRoomUnsafe(roomID, client)
	// Отправляем историю комнаты
	go h.sendHistory(client, roomID)
}
