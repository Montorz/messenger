package chat

import (
	"log"
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
				// личные сообщения (добавлю позже)
				log.Printf("Личное сообщение для %s (пока не реализовано)", msg.ToChatID)
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
