package chat

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Montorz/messenger/internal/storage"
	"github.com/Montorz/messenger/pkg/logger"
	"github.com/google/uuid"
)

type Hub struct {
	clients         map[uuid.UUID]*Client
	rooms           map[string]map[uuid.UUID]*Client
	userCurrentRoom map[string]string
	register        chan *Client
	unregister      chan *Client
	broadcast       chan *Message
	mu              sync.RWMutex
	db              *storage.DB
	logger          *logger.Logger
}

func NewHub(db *storage.DB, log *logger.Logger) *Hub {
	return &Hub{
		clients:         make(map[uuid.UUID]*Client),
		rooms:           make(map[string]map[uuid.UUID]*Client),
		userCurrentRoom: make(map[string]string),
		register:        make(chan *Client),
		unregister:      make(chan *Client),
		broadcast:       make(chan *Message, 256),
		db:              db,
		logger:          log,
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.ID] = client
			h.logger.Info("Клиент %s (%s) подключился", client.Username, client.ID.String()[:8])

			allRooms, _ := h.db.GetAllRooms()
			if len(allRooms) > 0 {
				roomList := "Доступные комнаты: "
				for _, r := range allRooms {
					roomList += r.Name + " "
				}
				client.Send <- &Message{
					ID:           uuid.New(),
					Type:         "system",
					ToChatID:     "user:" + client.Username,
					Content:      roomList,
					FromUsername: "system",
					CreatedAt:    time.Now(),
				}
			} else {
				client.Send <- &Message{
					ID:           uuid.New(),
					Type:         "system",
					ToChatID:     "user:" + client.Username,
					Content:      "Пока нет созданных комнат. Используйте /create <название> чтобы создать первую комнату!",
					FromUsername: "system",
					CreatedAt:    time.Now(),
				}
			}
			h.mu.Unlock()

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client.ID]; ok {
				delete(h.clients, client.ID)
				delete(h.userCurrentRoom, client.Username)
				h.logger.Info("Клиент %s отключился", client.Username)

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
			h.logger.Debug("Получено сообщение: type=%s, to=%s, from=%s", msg.Type, msg.ToChatID, msg.FromUsername)

			// CREATE ROOM
			if msg.Type == "create_room" {
				roomName := strings.TrimSpace(msg.Content)
				if roomName == "" {
					h.sendToUser(msg.FromUsername, &Message{
						ID:           uuid.New(),
						Type:         "system",
						ToChatID:     "user:" + msg.FromUsername,
						Content:      "Укажите название комнаты: /create <название>",
						FromUsername: "system",
						CreatedAt:    time.Now(),
					})
					continue
				}

				err := h.db.CreateRoom(roomName, msg.FromUsername)
				if err != nil {
					h.sendToUser(msg.FromUsername, &Message{
						ID:           uuid.New(),
						Type:         "system",
						ToChatID:     "user:" + msg.FromUsername,
						Content:      fmt.Sprintf("Комната '%s' уже существует", roomName),
						FromUsername: "system",
						CreatedAt:    time.Now(),
					})
					continue
				}

				roomID := "room:" + roomName
				h.mu.Lock()
				if _, ok := h.rooms[roomID]; !ok {
					h.rooms[roomID] = make(map[uuid.UUID]*Client)
				}
				var client *Client
				for _, c := range h.clients {
					if c.Username == msg.FromUsername {
						client = c
						break
					}
				}
				if client != nil {
					h.rooms[roomID][client.ID] = client
					h.userCurrentRoom[msg.FromUsername] = roomName
					h.logger.Info("Клиент %s добавлен в комнату %s", msg.FromUsername, roomName)
				}
				h.mu.Unlock()

				h.sendToUser(msg.FromUsername, &Message{
					ID:           uuid.New(),
					Type:         "system",
					ToChatID:     "user:" + msg.FromUsername,
					Content:      fmt.Sprintf("Комната '%s' создана! Вы вошли в неё. Теперь просто пишите текст.", roomName),
					FromUsername: "system",
					CreatedAt:    time.Now(),
				})

				h.broadcastToAll(&Message{
					ID:           uuid.New(),
					Type:         "system",
					ToChatID:     "system",
					Content:      fmt.Sprintf("Создана новая комната '%s' пользователем %s! Используйте /join %s", roomName, msg.FromUsername, roomName),
					FromUsername: "system",
					CreatedAt:    time.Now(),
				})
				continue
			}

			// JOIN ROOM
			if msg.Type == "join_room" {
				roomName := strings.TrimSpace(msg.Content)
				if roomName == "" {
					h.sendToUser(msg.FromUsername, &Message{
						ID:           uuid.New(),
						Type:         "system",
						ToChatID:     "user:" + msg.FromUsername,
						Content:      "Укажите название комнаты: /join <название>",
						FromUsername: "system",
						CreatedAt:    time.Now(),
					})
					continue
				}

				allRooms, _ := h.db.GetAllRooms()
				roomExists := false
				for _, r := range allRooms {
					if r.Name == roomName {
						roomExists = true
						break
					}
				}

				if !roomExists {
					h.sendToUser(msg.FromUsername, &Message{
						ID:           uuid.New(),
						Type:         "system",
						ToChatID:     "user:" + msg.FromUsername,
						Content:      fmt.Sprintf("Комната '%s' не существует", roomName),
						FromUsername: "system",
						CreatedAt:    time.Now(),
					})
					continue
				}

				h.db.JoinRoom(roomName, msg.FromUsername)

				roomID := "room:" + roomName
				h.mu.Lock()
				if _, ok := h.rooms[roomID]; !ok {
					h.rooms[roomID] = make(map[uuid.UUID]*Client)
				}
				var client *Client
				for _, c := range h.clients {
					if c.Username == msg.FromUsername {
						client = c
						break
					}
				}
				if client != nil {
					h.rooms[roomID][client.ID] = client
					h.userCurrentRoom[msg.FromUsername] = roomName
					h.logger.Info("Клиент %s присоединился к комнате %s", msg.FromUsername, roomName)
				}
				h.mu.Unlock()

				h.sendToUser(msg.FromUsername, &Message{
					ID:           uuid.New(),
					Type:         "system",
					ToChatID:     "user:" + msg.FromUsername,
					Content:      fmt.Sprintf("Вы вошли в комнату '%s'! Теперь всё, что вы пишете, будет уходить сюда.", roomName),
					FromUsername: "system",
					CreatedAt:    time.Now(),
				})

				go h.sendHistoryToUser(msg.FromUsername, roomID)
				continue
			}

			// LEAVE ROOM - ИСПРАВЛЕНО!
			if msg.Type == "leave_room" {
				roomName := strings.TrimSpace(msg.Content)
				if roomName == "" {
					h.sendToUser(msg.FromUsername, &Message{
						ID:           uuid.New(),
						Type:         "system",
						ToChatID:     "user:" + msg.FromUsername,
						Content:      "Укажите название комнаты: /leave <название>",
						FromUsername: "system",
						CreatedAt:    time.Now(),
					})
					continue
				}

				// Удаляем из БД
				h.db.LeaveRoom(roomName, msg.FromUsername)

				roomID := "room:" + roomName

				h.mu.Lock()
				// Удаляем пользователя из памяти
				if members, ok := h.rooms[roomID]; ok {
					var clientID uuid.UUID
					for id, client := range h.clients {
						if client.Username == msg.FromUsername {
							clientID = id
							break
						}
					}
					delete(members, clientID)
					h.logger.Info("Клиент %s удалён из комнаты %s в памяти", msg.FromUsername, roomName)

					if len(members) == 0 {
						delete(h.rooms, roomID)
					}
				}

				// Очищаем текущую комнату у пользователя
				if currentRoom, ok := h.userCurrentRoom[msg.FromUsername]; ok && currentRoom == roomName {
					delete(h.userCurrentRoom, msg.FromUsername)
					h.logger.Info("Клиент %s вышел из комнаты %s, текущая комната очищена", msg.FromUsername, roomName)
				}
				h.mu.Unlock()

				h.sendToUser(msg.FromUsername, &Message{
					ID:           uuid.New(),
					Type:         "system",
					ToChatID:     "user:" + msg.FromUsername,
					Content:      fmt.Sprintf("Вы вышли из комнаты '%s'. Вы больше не будете получать сообщения из этой комнаты.", roomName),
					FromUsername: "system",
					CreatedAt:    time.Now(),
				})
				continue
			}

			// LIST ROOMS
			if msg.Type == "list_rooms" {
				allRooms, err := h.db.GetAllRooms()
				if err != nil {
					continue
				}

				if len(allRooms) == 0 {
					h.sendToUser(msg.FromUsername, &Message{
						ID:           uuid.New(),
						Type:         "system",
						ToChatID:     "user:" + msg.FromUsername,
						Content:      "Нет созданных комнат. Создайте первую: /create <название>",
						FromUsername: "system",
						CreatedAt:    time.Now(),
					})
				} else {
					roomNames := make([]string, len(allRooms))
					for i, r := range allRooms {
						roomNames[i] = r.Name
					}
					content := fmt.Sprintf("Доступные комнаты (%d): %s\nИспользуйте /join <название> чтобы войти", len(allRooms), strings.Join(roomNames, ", "))
					h.sendToUser(msg.FromUsername, &Message{
						ID:           uuid.New(),
						Type:         "system",
						ToChatID:     "user:" + msg.FromUsername,
						Content:      content,
						FromUsername: "system",
						CreatedAt:    time.Now(),
					})
				}
				continue
			}

			// CURRENT ROOM
			if msg.Type == "current_room" {
				h.mu.RLock()
				currentRoom := h.userCurrentRoom[msg.FromUsername]
				h.mu.RUnlock()

				if currentRoom != "" {
					h.sendToUser(msg.FromUsername, &Message{
						ID:           uuid.New(),
						Type:         "system",
						ToChatID:     "user:" + msg.FromUsername,
						Content:      fmt.Sprintf("Вы находитесь в комнате '%s'", currentRoom),
						FromUsername: "system",
						CreatedAt:    time.Now(),
					})
				} else {
					h.sendToUser(msg.FromUsername, &Message{
						ID:           uuid.New(),
						Type:         "system",
						ToChatID:     "user:" + msg.FromUsername,
						Content:      "Вы не в комнате. Используйте /join <название> чтобы войти",
						FromUsername: "system",
						CreatedAt:    time.Now(),
					})
				}
				continue
			}

			// GET USERS
			if msg.Type == "get_users" {
				h.mu.RLock()
				var users []string
				for _, client := range h.clients {
					users = append(users, client.Username)
				}
				onlineCount := len(users)
				h.mu.RUnlock()

				content := fmt.Sprintf("Онлайн (%d): %s", onlineCount, strings.Join(users, ", "))
				h.sendToUser(msg.FromUsername, &Message{
					ID:           uuid.New(),
					Type:         "system",
					ToChatID:     "user:" + msg.FromUsername,
					Content:      content,
					FromUsername: "system",
					CreatedAt:    time.Now(),
				})
				continue
			}

			// REGULAR MESSAGE TO CURRENT ROOM
			if msg.Type == "group" && msg.ToChatID == "" {
				h.mu.RLock()
				currentRoomName, hasRoom := h.userCurrentRoom[msg.FromUsername]
				h.mu.RUnlock()

				if !hasRoom {
					h.sendToUser(msg.FromUsername, &Message{
						ID:           uuid.New(),
						Type:         "system",
						ToChatID:     "user:" + msg.FromUsername,
						Content:      "Вы не в комнате. Используйте /join <название> чтобы войти в комнату, или /create <название> чтобы создать новую",
						FromUsername: "system",
						CreatedAt:    time.Now(),
					})
					continue
				}

				roomID := "room:" + currentRoomName
				msg.ToChatID = roomID

				go func(m *Message) {
					if err := h.db.SaveMessage(m.FromUserID, m.ToChatID, m.Content, nil); err != nil {
						h.logger.Error("Ошибка сохранения: %v", err)
					}
				}(msg)

				h.mu.RLock()
				members, ok := h.rooms[roomID]
				h.mu.RUnlock()

				if ok {
					for _, client := range members {
						select {
						case client.Send <- msg:
						default:
						}
					}
				}
				continue
			}

			// PRIVATE MESSAGE
			if IsPrivateChat(msg.ToChatID) {
				username := msg.ToChatID[5:]

				go func(m *Message) {
					if err := h.db.SaveMessage(m.FromUserID, m.ToChatID, m.Content, nil); err != nil {
						h.logger.Error("Ошибка сохранения: %v", err)
					}
				}(msg)

				h.sendToUser(username, msg)
				continue
			}
		}
	}
}

func (h *Hub) sendToUser(username string, msg *Message) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, client := range h.clients {
		if client.Username == username {
			select {
			case client.Send <- msg:
			default:
				h.logger.Warn("Клиент %s недоступен", username)
			}
			return
		}
	}
}

func (h *Hub) broadcastToAll(msg *Message) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, client := range h.clients {
		select {
		case client.Send <- msg:
		default:
		}
	}
}

func (h *Hub) sendHistoryToUser(username, chatID string) {
	history, err := h.db.GetChatHistory(chatID, 50)
	if err != nil {
		return
	}

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
		h.sendToUser(username, historyMsg)
	}
}
