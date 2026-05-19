package chat

import (
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// Hub управляет всеми подключениями
type Hub struct {
	clients    map[uuid.UUID]*Client
	rooms      map[string]map[uuid.UUID]*Client
	register   chan *Client
	unregister chan *Client
	broadcast  chan *Message
	mu         sync.RWMutex
}

// NewHub создаёт новый хаб
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[uuid.UUID]*Client),
		rooms:      make(map[string]map[uuid.UUID]*Client),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan *Message, 256),
	}
}

func (h *Hub) findClientByUsername(username string) (*Client, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, client := range h.clients {
		if client.Username == username {
			return client, true
		}
	}
	return nil, false
}

// Run запускает основной цикл хаба
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.ID] = client
			log.Printf("Клиент %s (%s) подключился", client.Username, client.ID)
			h.mu.Unlock()

			// Автоматически добавляем в общую комнату (для тестирования)
			h.JoinRoom("group:general", client)

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
			if msg.Type == "get_users" {
				h.mu.RLock()
				var users []string
				for _, client := range h.clients {
					users = append(users, client.Username)
				}
				h.mu.RUnlock()

				// Отправляем список только запросившему
				if client, ok := h.clients[msg.FromUserID]; ok {
					response := &Message{
						Type:     "system",
						ToChatID: "user:" + msg.FromUserID.String(),
						Content:  fmt.Sprintf("Онлайн: %s", strings.Join(users, ", ")),
					}
					client.Send <- response
				}
				continue
			}

			h.mu.RLock()
			log.Printf("Получено сообщение: type=%s, to=%s, from=%s", msg.Type, msg.ToChatID, msg.FromUsername)

			// Если сообщение адресовано комнате
			if IsGroupChat(msg.ToChatID) {
				if members, ok := h.rooms[msg.ToChatID]; ok {
					log.Printf("Отправляем в комнату %s, кол-во участников: %d", msg.ToChatID, len(members))
					for _, client := range members {
						select {
						case client.Send <- msg:
							log.Printf("Сообщение отправлено клиенту %s", client.Username)
						default:
							log.Printf("Клиент %s недоступен, закрываем", client.Username)
							close(client.Send)
							delete(h.clients, client.ID)
						}
					}
				} else {
					log.Printf("Комната %s не найдена", msg.ToChatID)
				}
			} else if IsPrivateChat(msg.ToChatID) {
				// msg.ToChatID = "user:username" (например "user:alice")
				username := msg.ToChatID[5:] // убираем "user:"

				// Ищем клиента по username
				targetClient, found := h.findClientByUsername(username)

				if found {
					select {
					case targetClient.Send <- msg:
						log.Printf("Личное сообщение отправлено пользователю %s", username)
					default:
						log.Printf("Получатель %s недоступен", username)
					}
				} else {
					log.Printf("Пользователь %s не в сети", username)
					// Отправляем отправителю уведомление
					if sender, ok := h.clients[msg.FromUserID]; ok {
						errorMsg := &Message{
							Type:     "system",
							ToChatID: "user:" + sender.Username,
							Content:  fmt.Sprintf("Пользователь %s не в сети", username),
						}
						sender.Send <- errorMsg
					}
				}
			}

			h.mu.RUnlock()
		}
	}
}

// JoinRoom добавляет клиента в комнату
func (h *Hub) JoinRoom(roomID string, client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.rooms[roomID]; !ok {
		h.rooms[roomID] = make(map[uuid.UUID]*Client)
		log.Printf("Создана новая комната: %s", roomID)
	}
	h.rooms[roomID][client.ID] = client
	log.Printf("Клиент %s присоединился к комнате %s", client.Username, roomID)
}
