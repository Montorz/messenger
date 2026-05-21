package storage

import (
	"database/sql"
	"log"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

type User struct {
	ID        uuid.UUID
	Username  string
	Password  string
	CreatedAt time.Time
	LastSeen  time.Time
}

type Room struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

type DB struct {
	conn *sql.DB
}

func NewDB(dbPath string) (*DB, error) {
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	if err := conn.Ping(); err != nil {
		return nil, err
	}

	if err := createTables(conn); err != nil {
		return nil, err
	}

	log.Println("Подключение к SQLite установлено")
	return &DB{conn: conn}, nil
}

func createTables(conn *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS users (
            id TEXT PRIMARY KEY,
            username TEXT UNIQUE NOT NULL,
            password TEXT NOT NULL,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            last_seen DATETIME DEFAULT CURRENT_TIMESTAMP
        )`,
		`CREATE TABLE IF NOT EXISTS rooms (
            id TEXT PRIMARY KEY,
            name TEXT UNIQUE NOT NULL,
            created_by TEXT NOT NULL,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (created_by) REFERENCES users(username)
        )`,
		`CREATE TABLE IF NOT EXISTS room_members (
            room_id TEXT NOT NULL,
            username TEXT NOT NULL,
            joined_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            PRIMARY KEY (room_id, username),
            FOREIGN KEY (room_id) REFERENCES rooms(id),
            FOREIGN KEY (username) REFERENCES users(username)
        )`,
		`CREATE TABLE IF NOT EXISTS messages (
            id TEXT PRIMARY KEY,
            from_user_id TEXT NOT NULL,
            to_chat_id TEXT NOT NULL,
            content TEXT NOT NULL,
            reply_to_id TEXT,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (from_user_id) REFERENCES users(id),
            FOREIGN KEY (reply_to_id) REFERENCES messages(id)
        )`,
		`CREATE INDEX IF NOT EXISTS idx_messages_to_chat_id ON messages(to_chat_id)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_created_at ON messages(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_room_members_username ON room_members(username)`,
	}

	for _, query := range queries {
		if _, err := conn.Exec(query); err != nil {
			log.Printf("Ошибка создания таблицы: %v", err)
			return err
		}
	}
	log.Println("Таблицы БД созданы/проверены")
	return nil
}

func (db *DB) CreateUser(username, password string) (*User, error) {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	user := &User{
		ID:        uuid.New(),
		Username:  username,
		Password:  string(hashedPassword),
		CreatedAt: time.Now(),
		LastSeen:  time.Now(),
	}

	_, err = db.conn.Exec(
		"INSERT INTO users (id, username, password, created_at, last_seen) VALUES (?, ?, ?, ?, ?)",
		user.ID.String(), user.Username, user.Password, user.CreatedAt, user.LastSeen,
	)
	if err != nil {
		return nil, err
	}

	log.Printf("Создан пользователь: %s", username)
	return user, nil
}

func (db *DB) GetUserByUsername(username string) (*User, error) {
	var user User
	var idStr string

	err := db.conn.QueryRow(
		"SELECT id, username, password, created_at, last_seen FROM users WHERE username = ?",
		username,
	).Scan(&idStr, &user.Username, &user.Password, &user.CreatedAt, &user.LastSeen)

	if err != nil {
		return nil, err
	}

	user.ID, err = uuid.Parse(idStr)
	return &user, err
}

func (db *DB) CheckPassword(username, password string) (*User, error) {
	user, err := db.GetUserByUsername(username)
	if err != nil {
		return nil, err
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password))
	if err != nil {
		return nil, err
	}

	return user, nil
}

func (db *DB) UpdateLastSeen(userID uuid.UUID) error {
	_, err := db.conn.Exec(
		"UPDATE users SET last_seen = CURRENT_TIMESTAMP WHERE id = ?",
		userID.String(),
	)
	return err
}

func (db *DB) SaveMessage(fromUserID uuid.UUID, toChatID, content string, replyToID *uuid.UUID) error {
	id := uuid.New()

	var replyIDStr interface{}
	if replyToID != nil {
		replyIDStr = replyToID.String()
	}

	_, err := db.conn.Exec(
		"INSERT INTO messages (id, from_user_id, to_chat_id, content, reply_to_id, created_at) VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)",
		id.String(), fromUserID.String(), toChatID, content, replyIDStr,
	)
	if err != nil {
		log.Printf("Ошибка сохранения сообщения: %v", err)
		return err
	}

	log.Printf("Сообщение сохранено в БД: %s -> %s", fromUserID.String()[:8], toChatID)
	return nil
}

func (db *DB) GetChatHistory(chatID string, limit int) ([]map[string]interface{}, error) {
	rows, err := db.conn.Query(`
        SELECT m.id, m.content, m.created_at, u.username as from_username, m.from_user_id
        FROM messages m
        JOIN users u ON m.from_user_id = u.id
        WHERE m.to_chat_id = ?
        ORDER BY m.created_at DESC
        LIMIT ?
    `, chatID, limit)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []map[string]interface{}
	for rows.Next() {
		var id, content, fromUsername, fromUserID string
		var createdAt time.Time

		if err := rows.Scan(&id, &content, &createdAt, &fromUsername, &fromUserID); err != nil {
			continue
		}

		messages = append([]map[string]interface{}{
			{
				"id":            id,
				"content":       content,
				"from_username": fromUsername,
				"from_user_id":  fromUserID,
				"created_at":    createdAt,
			},
		}, messages...)
	}

	return messages, nil
}

// CreateRoom создаёт новую комнату
func (db *DB) CreateRoom(roomName, createdBy string) error {
	roomID := "room:" + roomName

	_, err := db.conn.Exec(
		"INSERT INTO rooms (id, name, created_by) VALUES (?, ?, ?)",
		roomID, roomName, createdBy,
	)
	if err != nil {
		return err
	}

	// Добавляем создателя в комнату
	_, err = db.conn.Exec(
		"INSERT INTO room_members (room_id, username) VALUES (?, ?)",
		roomID, createdBy,
	)
	return err
}

// JoinRoom добавляет пользователя в комнату
func (db *DB) JoinRoom(roomName, username string) error {
	roomID := "room:" + roomName

	// Проверяем существует ли комната
	var exists bool
	err := db.conn.QueryRow("SELECT EXISTS(SELECT 1 FROM rooms WHERE name = ?)", roomName).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return sql.ErrNoRows
	}

	// Добавляем пользователя
	_, err = db.conn.Exec(
		"INSERT OR IGNORE INTO room_members (room_id, username) VALUES (?, ?)",
		roomID, username,
	)
	return err
}

// LeaveRoom удаляет пользователя из комнаты
func (db *DB) LeaveRoom(roomName, username string) error {
	roomID := "room:" + roomName

	_, err := db.conn.Exec(
		"DELETE FROM room_members WHERE room_id = ? AND username = ?",
		roomID, username,
	)
	return err
}

// GetAllRooms возвращает ВСЕ существующие комнаты (для списка доступных)
func (db *DB) GetAllRooms() ([]Room, error) {
	rows, err := db.conn.Query(`
        SELECT id, name, created_by, created_at FROM rooms ORDER BY name
    `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rooms []Room
	for rows.Next() {
		var room Room
		if err := rows.Scan(&room.ID, &room.Name, &room.CreatedBy, &room.CreatedAt); err != nil {
			continue
		}
		rooms = append(rooms, room)
	}
	return rooms, nil
}

// GetUserRooms возвращает комнаты, в которых состоит пользователь
func (db *DB) GetUserRooms(username string) ([]Room, error) {
	rows, err := db.conn.Query(`
        SELECT r.id, r.name, r.created_by, r.created_at
        FROM rooms r
        JOIN room_members rm ON r.id = rm.room_id
        WHERE rm.username = ?
        ORDER BY r.name
    `, username)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rooms []Room
	for rows.Next() {
		var room Room
		if err := rows.Scan(&room.ID, &room.Name, &room.CreatedBy, &room.CreatedAt); err != nil {
			continue
		}
		rooms = append(rooms, room)
	}
	return rooms, nil
}

// IsRoomMember проверяет, состоит ли пользователь в комнате
func (db *DB) IsRoomMember(roomName, username string) (bool, error) {
	roomID := "room:" + roomName
	var exists bool
	err := db.conn.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM room_members WHERE room_id = ? AND username = ?)",
		roomID, username,
	).Scan(&exists)
	return exists, err
}

func (db *DB) Close() error {
	return db.conn.Close()
}
