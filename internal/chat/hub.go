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

// Hub - центральный управляющий компонент мессенджера
type Hub struct {
	// Хранилища данных (защищены мьютексом)
	clients         map[uuid.UUID]*Client            // Все подключённые клиенты
	rooms           map[string]map[uuid.UUID]*Client // Комнаты и их участники
	userCurrentRoom map[string]string                // Текущая комната каждого пользователя

	// Каналы для коммуникации с горутинами
	register   chan *Client  // Новые клиенты
	unregister chan *Client  // Отключающиеся клиенты
	broadcast  chan *Message // Входящие сообщения от клиентов

	// Синхронизация и сервисы
	mu     sync.RWMutex   // Защита maps
	db     *storage.DB    // База данных
	logger *logger.Logger // Логгер
}

// NewHub - создаёт новый экземпляр Hub
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

// Run - главный цикл обработки событий хаба
func (h *Hub) Run() {
	for {
		select {
		// 1: РЕГИСТРАЦИЯ НОВОГО КЛИЕНТА
		case client := <-h.register:
			h.mu.Lock() // Блокируем для записи в maps

			// Добавляем клиента в общий список
			h.clients[client.ID] = client
			h.logger.Info("Клиент %s (%s) подключился", client.Username, client.ID.String()[:8])

			// Получаем список всех комнат из БД
			allRooms, _ := h.db.GetAllRooms()

			if len(allRooms) > 0 {
				// Формируем список комнат
				roomList := "Доступные комнаты: "
				for _, r := range allRooms {
					roomList += r.Name + " "
				}
				// Отправляем приветственное сообщение
				client.Send <- &Message{
					ID:           uuid.New(),
					Type:         "system",
					ToChatID:     "user:" + client.Username,
					Content:      roomList,
					FromUsername: "system",
					CreatedAt:    time.Now(),
				}
			} else {
				// Нет комнат - подсказываем как создать
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

		// 2: ОТКЛЮЧЕНИЕ КЛИЕНТА
		case client := <-h.unregister:
			h.mu.Lock()

			// Проверяем, существует ли клиент
			if _, ok := h.clients[client.ID]; ok {
				// Удаляем из общего списка
				delete(h.clients, client.ID)
				delete(h.userCurrentRoom, client.Username)
				h.logger.Info("Клиент %s отключился", client.Username)

				// Удаляем клиента из всех комнат, где он состоял
				for roomID, members := range h.rooms {
					if _, ok := members[client.ID]; ok {
						delete(members, client.ID)
						// Если комната опустела - удаляем её
						if len(members) == 0 {
							delete(h.rooms, roomID)
						}
					}
				}
			}
			h.mu.Unlock()

			// Закрываем канал отправки
			close(client.Send)

		// 3: ВХОДЯЩЕЕ СООБЩЕНИЕ ОТ КЛИЕНТА
		case msg := <-h.broadcast:
			h.logger.Debug("Получено сообщение: type=%s, to=%s, from=%s",
				msg.Type, msg.ToChatID, msg.FromUsername)

			// СОЗДАНИЕ КОМНАТЫ (/create)
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

				// Создаём комнату в БД
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

				// Автоматически заходим в созданную комнату
				roomID := "room:" + roomName
				h.mu.Lock()
				if _, ok := h.rooms[roomID]; !ok {
					h.rooms[roomID] = make(map[uuid.UUID]*Client)
				}
				// Находим клиента по username
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

				// Подтверждение создания
				h.sendToUser(msg.FromUsername, &Message{
					ID:           uuid.New(),
					Type:         "system",
					ToChatID:     "user:" + msg.FromUsername,
					Content:      fmt.Sprintf("Комната '%s' создана! Вы вошли в неё. Теперь просто пишите текст.", roomName),
					FromUsername: "system",
					CreatedAt:    time.Now(),
				})

				// Оповещаем всех пользователей о новой комнате
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

			// ВХОД В КОМНАТУ (/join)
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

				// Проверяем существование комнаты
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

				// Добавляем в БД
				h.db.JoinRoom(roomName, msg.FromUsername)

				// Добавляем в память
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

				// Подтверждение входа
				h.sendToUser(msg.FromUsername, &Message{
					ID:           uuid.New(),
					Type:         "system",
					ToChatID:     "user:" + msg.FromUsername,
					Content:      fmt.Sprintf("Вы вошли в комнату '%s'! Теперь всё, что вы пишете, будет уходить сюда.", roomName),
					FromUsername: "system",
					CreatedAt:    time.Now(),
				})

				// Отправляем историю комнаты
				go h.sendHistoryToUser(msg.FromUsername, roomID)
				continue
			}

			// ВЫХОД ИЗ КОМНАТЫ (/leave)
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
				// Удаляем из памяти
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

			// СПИСОК ВСЕХ КОМНАТ (/rooms)
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
					content := fmt.Sprintf("Доступные комнаты (%d): %s\nИспользуйте /join <название> чтобы войти",
						len(allRooms), strings.Join(roomNames, ", "))
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

			// ПОКАЗАТЬ ТЕКУЩУЮ КОМНАТУ (/room)
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

			// СПИСОК ОНЛАЙН ПОЛЬЗОВАТЕЛЕЙ (/users)
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

			// ОБЫЧНОЕ СООБЩЕНИЕ В ТЕКУЩУЮ КОМНАТУ (просто текст)
			if msg.Type == "group" && msg.ToChatID == "" {
				h.mu.RLock()
				currentRoomName, hasRoom := h.userCurrentRoom[msg.FromUsername]
				h.mu.RUnlock()

				// Проверяем, находится ли пользователь в какой-либо комнате
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

				// Сохраняем сообщение в БД (в отдельной горутине, чтобы не блокировать)
				go func(m *Message) {
					if err := h.db.SaveMessage(m.FromUserID, m.ToChatID, m.Content, nil); err != nil {
						h.logger.Error("Ошибка сохранения: %v", err)
					}
				}(msg)

				// Отправляем всем участникам комнаты, КРОМЕ отправителя
				h.mu.RLock()
				members, ok := h.rooms[roomID]
				h.mu.RUnlock()

				if ok {
					for _, client := range members {
						// Пропускаем отправителя (свои сообщения не присылаем)
						if client.Username == msg.FromUsername {
							continue
						}
						select {
						case client.Send <- msg:
						default:
							// Канал заполнен - клиент недоступен
						}
					}
				}
				continue
			}

			// ИСТОРИЯ ЛИЧНОЙ ПЕРЕПИСКИ (/history username)
			if msg.Type == "history" {
				targetUsername := strings.TrimSpace(msg.Content)
				if targetUsername == "" {
					h.sendToUser(msg.FromUsername, &Message{
						ID:           uuid.New(),
						Type:         "system",
						ToChatID:     "user:" + msg.FromUsername,
						Content:      "Укажите имя пользователя: /history <username>",
						FromUsername: "system",
						CreatedAt:    time.Now(),
					})
					continue
				}

				// Проверяем существование пользователя
				_, err := h.db.GetUserByUsername(targetUsername)
				if err != nil {
					h.sendToUser(msg.FromUsername, &Message{
						ID:           uuid.New(),
						Type:         "system",
						ToChatID:     "user:" + msg.FromUsername,
						Content:      fmt.Sprintf("Пользователь '%s' не найден", targetUsername),
						FromUsername: "system",
						CreatedAt:    time.Now(),
					})
					continue
				}

				// Получаем историю из БД
				history, err := h.db.GetPrivateChatHistory(msg.FromUsername, targetUsername, 50)
				if err != nil {
					h.logger.Error("Ошибка получения истории: %v", err)
					h.sendToUser(msg.FromUsername, &Message{
						ID:           uuid.New(),
						Type:         "system",
						ToChatID:     "user:" + msg.FromUsername,
						Content:      fmt.Sprintf("Ошибка получения истории: %v", err),
						FromUsername: "system",
						CreatedAt:    time.Now(),
					})
					continue
				}

				if len(history) == 0 {
					h.sendToUser(msg.FromUsername, &Message{
						ID:           uuid.New(),
						Type:         "system",
						ToChatID:     "user:" + msg.FromUsername,
						Content:      fmt.Sprintf("История переписки с %s пуста", targetUsername),
						FromUsername: "system",
						CreatedAt:    time.Now(),
					})
					continue
				}

				// Заголовок истории
				h.sendToUser(msg.FromUsername, &Message{
					ID:           uuid.New(),
					Type:         "system",
					ToChatID:     "user:" + msg.FromUsername,
					Content:      fmt.Sprintf("----- История переписки с %s (последние %d сообщений) -----", targetUsername, len(history)),
					FromUsername: "system",
					CreatedAt:    time.Now(),
				})

				// Каждое сообщение истории
				for _, msgHistory := range history {
					createdAt, ok := msgHistory["created_at"].(time.Time)
					if !ok {
						continue
					}
					fromUsername, ok := msgHistory["from_username"].(string)
					if !ok {
						continue
					}
					content, ok := msgHistory["content"].(string)
					if !ok {
						continue
					}

					timeStr := createdAt.Format("02.01.2006 15:04:05")

					// Определяем направление сообщения
					var direction string
					if fromUsername == msg.FromUsername {
						direction = "->" // Исходящее
					} else {
						direction = "<-" // Входящее
					}

					historyMsg := &Message{
						ID:           uuid.New(),
						Type:         "system",
						FromUserID:   uuid.New(),
						FromUsername: fromUsername,
						ToChatID:     "user:" + msg.FromUsername,
						Content:      fmt.Sprintf("[%s] %s %s: %s", timeStr, fromUsername, direction, content),
						CreatedAt:    createdAt,
					}
					h.sendToUser(msg.FromUsername, historyMsg)
				}

				// Разделитель
				h.sendToUser(msg.FromUsername, &Message{
					ID:           uuid.New(),
					Type:         "system",
					ToChatID:     "user:" + msg.FromUsername,
					Content:      "-----------------------------------------------------",
					FromUsername: "system",
					CreatedAt:    time.Now(),
				})
				continue
			}

			// ЛИЧНОЕ СООБЩЕНИЕ (/msg username текст)
			if IsPrivateChat(msg.ToChatID) {
				// Извлекаем имя получателя из "user:username"
				username := msg.ToChatID[5:]

				// Сохраняем в БД (отдельная горутина)
				go func(m *Message) {
					if err := h.db.SaveMessage(m.FromUserID, m.ToChatID, m.Content, nil); err != nil {
						h.logger.Error("Ошибка сохранения: %v", err)
					}
				}(msg)

				// Отправляем получателю
				h.sendToUser(username, msg)
				continue
			}
		}
	}
}

// sendToUser - отправляет сообщение конкретному пользователю по имени
func (h *Hub) sendToUser(username string, msg *Message) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, client := range h.clients {
		if client.Username == username {
			select {
			case client.Send <- msg:
				// Отправлено успешно
			default:
				// Канал заполнен - клиент не успевает читать
				h.logger.Warn("Клиент %s недоступен", username)
			}
			return
		}
	}
}

// broadcastToAll - отправляет сообщение всем подключённым клиентам
func (h *Hub) broadcastToAll(msg *Message) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, client := range h.clients {
		select {
		case client.Send <- msg:
			// Отправлено
		default:
			// Клиент недоступен - пропускаем
		}
	}
}

// sendHistoryToUser - отправляет пользователю историю сообщений комнаты
func (h *Hub) sendHistoryToUser(username, chatID string) {
	// Получаем историю из БД (последние 50 сообщений)
	history, err := h.db.GetChatHistory(chatID, 50)
	if err != nil {
		h.logger.Error("Ошибка получения истории: %v", err)
		return
	}

	// Если истории нет - сообщаем об этом
	if len(history) == 0 {
		h.sendToUser(username, &Message{
			ID:           uuid.New(),
			Type:         "system",
			ToChatID:     "user:" + username,
			Content:      "История сообщений пуста.",
			FromUsername: "system",
			CreatedAt:    time.Now(),
		})
		return
	}

	// Заголовок
	h.sendToUser(username, &Message{
		ID:           uuid.New(),
		Type:         "system",
		ToChatID:     "user:" + username,
		Content:      fmt.Sprintf("----- История комнаты (последние %d сообщений) ----", len(history)),
		FromUsername: "system",
		CreatedAt:    time.Now(),
	})

	// Каждое сообщение
	for _, msg := range history {
		// Извлекаем поля с проверкой типов
		createdAt, ok := msg["created_at"].(time.Time)
		if !ok {
			continue
		}
		fromUsername, ok := msg["from_username"].(string)
		if !ok {
			continue
		}
		content, ok := msg["content"].(string)
		if !ok {
			continue
		}

		timeStr := createdAt.Format("02.01.2006 15:04:05")

		historyMsg := &Message{
			ID:           uuid.New(),
			Type:         "system",
			FromUserID:   uuid.New(),
			FromUsername: fromUsername,
			ToChatID:     chatID,
			Content:      fmt.Sprintf("[%s] %s: %s", timeStr, fromUsername, content),
			CreatedAt:    createdAt,
		}
		h.sendToUser(username, historyMsg)
	}

	// Разделитель
	h.sendToUser(username, &Message{
		ID:           uuid.New(),
		Type:         "system",
		ToChatID:     "user:" + username,
		Content:      "-----------------------------------------------------",
		FromUsername: "system",
		CreatedAt:    time.Now(),
	})
}
