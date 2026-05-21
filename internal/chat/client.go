package chat

import (
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Client struct {
	ID         uuid.UUID
	Username   string
	UserID     uuid.UUID
	Conn       *websocket.Conn
	Hub        *Hub
	Send       chan *Message
	LastActive time.Time
}

var upgrader = websocket.Upgrader{
	CheckOrigin:     func(r *http.Request) bool { return true },
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func (c *Client) readPump() {
	defer func() {
		c.Hub.unregister <- c
		c.Conn.Close()
	}()

	for {
		var msg Message
		err := c.Conn.ReadJSON(&msg)
		if err != nil {
			break
		}

		msg.FromUserID = c.UserID
		msg.FromUsername = c.Username
		msg.CreatedAt = time.Now()

		c.Hub.broadcast <- &msg
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.Send:
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteJSON(msg); err != nil {
				return
			}

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func ServeWs(hub *Hub, w http.ResponseWriter, r *http.Request, userID uuid.UUID, username string) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Ошибка upgrade:", err)
		return
	}

	client := &Client{
		ID:       uuid.New(),
		Username: username,
		UserID:   userID,
		Conn:     conn,
		Hub:      hub,
		Send:     make(chan *Message, 256),
	}

	hub.register <- client

	go client.writePump()
	go client.readPump()
}
