package chat

import (
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// Client - структура, представляющая подключённого клиента
type Client struct {
	ID         uuid.UUID       // Уникальный ID клиента
	Username   string          // Имя пользователя
	UserID     uuid.UUID       // UUID пользователя из БД
	Conn       *websocket.Conn // WebSocket соединение
	Hub        *Hub            // Ссылка на хаб
	Send       chan *Message   // Канал для исходящих сообщений
	LastActive time.Time       // Время последнего сообщения
}

// upgrader - конфигурация для преобразования HTTP в WebSocket
var upgrader = websocket.Upgrader{
	CheckOrigin:     func(r *http.Request) bool { return true }, // Разрешаем все домены/адреса, с котороых пришёл запрос
	ReadBufferSize:  1024,                                       // 1KB достаточно для сообщений мессенджера
	WriteBufferSize: 1024,                                       // 1KB достаточно для сообщений мессенджера
}

// readPump - горутина для чтения сообщений от клиента
func (c *Client) readPump() {
	// Defer гарантирует выполнение очистку
	defer func() {
		c.Hub.unregister <- c // Уведомляем хаб об отключении клиента
		c.Conn.Close()        // Закрываем WebSocket соединение
	}()

	// Бесконечный цикл чтения сообщений
	for {
		var msg Message
		// Читаем JSON сообщение из WebSocket
		err := c.Conn.ReadJSON(&msg)
		if err != nil {
			break
		}

		// Заполняем информацию об отправителе
		msg.FromUserID = c.UserID
		msg.FromUsername = c.Username
		msg.CreatedAt = time.Now()

		// Отправляем сообщение в хаб для дальнейшей обработки
		c.Hub.broadcast <- &msg
	}
}

// writePump - горутина для отправки сообщений клиенту
func (c *Client) writePump() {
	// Таймер для отправки PING сообщений
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		// Получено сообщение для отправки
		case msg, ok := <-c.Send:
			if !ok {
				// Канал закрыт - отправляем CloseMessage и завершаемся
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			// Устанавливаем таймаут на запись
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))

			// Отправляем JSON сообщение клиенту
			if err := c.Conn.WriteJSON(msg); err != nil {
				return
			}

		// Сработал таймер PING
		case <-ticker.C:
			// Устанавливаем таймаут на запись PING
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))

			// Отправляем PING сообщение
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ServeWs - обрабатывает HTTP запрос и выполняет upgrade до WebSocket
func ServeWs(hub *Hub, w http.ResponseWriter, r *http.Request, userID uuid.UUID, username string) {
	// Выполняем upgrade HTTP -> WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Ошибка upgrade:", err)
		return
	}

	// Создаём структуру клиента
	client := &Client{
		ID:       uuid.New(),
		Username: username,
		UserID:   userID,
		Conn:     conn,
		Hub:      hub,
		Send:     make(chan *Message, 256),
	}

	// Регистрируем клиента в хабе хаб обработает регистрацию в своём главном цикле
	hub.register <- client

	// Запускаем две горутины для клиента
	go client.writePump()
	go client.readPump()
}
