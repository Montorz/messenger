package storage

import (
	"database/sql"
	"log"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

// User представляет пользователя в системе
type User struct {
	ID        uuid.UUID `json:"id"`
	Username  string    `json:"username"`
	Password  string    `json:"-"` // не отправляем в JSON
	CreatedAt time.Time `json:"created_at"`
	LastSeen  time.Time `json:"last_seen"`
}

// DB обёртка над sql.DB
type DB struct {
	conn *sql.DB
}

// NewDB создаёт новое подключение к БД
func NewDB(dbPath string) (*DB, error) {
	conn, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}

	// Проверяем подключение
	if err := conn.Ping(); err != nil {
		return nil, err
	}

	// Создаём таблицы
	if err := createTables(conn); err != nil {
		return nil, err
	}

	return &DB{conn: conn}, nil
}

// createTables создаёт необходимые таблицы
func createTables(conn *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS users (
            id TEXT PRIMARY KEY,
            username TEXT UNIQUE NOT NULL,
            password TEXT NOT NULL,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            last_seen DATETIME DEFAULT CURRENT_TIMESTAMP
        )`,
		`CREATE INDEX IF NOT EXISTS idx_users_username ON users(username)`,

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
	}

	for _, query := range queries {
		if _, err := conn.Exec(query); err != nil {
			return err
		}
	}

	log.Println("Таблицы БД созданы/проверены")
	return nil
}

// CreateUser создаёт нового пользователя
func (db *DB) CreateUser(username, password string) (*User, error) {
	// Хешируем пароль
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

	log.Printf("Создан пользователь: %s (%s)", username, user.ID)
	return user, nil
}

// GetUserByUsername получает пользователя по имени
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
	if err != nil {
		return nil, err
	}

	return &user, nil
}

// GetUserByID получает пользователя по ID
func (db *DB) GetUserByID(id uuid.UUID) (*User, error) {
	var user User
	var idStr string

	err := db.conn.QueryRow(
		"SELECT id, username, password, created_at, last_seen FROM users WHERE id = ?",
		id.String(),
	).Scan(&idStr, &user.Username, &user.Password, &user.CreatedAt, &user.LastSeen)

	if err != nil {
		return nil, err
	}

	user.ID, err = uuid.Parse(idStr)
	if err != nil {
		return nil, err
	}

	return &user, nil
}

// UpdateLastSeen обновляет время последнего визита
func (db *DB) UpdateLastSeen(userID uuid.UUID) error {
	_, err := db.conn.Exec(
		"UPDATE users SET last_seen = CURRENT_TIMESTAMP WHERE id = ?",
		userID.String(),
	)
	return err
}

// SaveMessage сохраняет сообщение в БД
func (db *DB) SaveMessage(msgID uuid.UUID, fromUserID uuid.UUID, toChatID, content string, replyToID *uuid.UUID) error {
	var replyIDStr interface{}
	if replyToID != nil {
		replyIDStr = replyToID.String()
	}

	_, err := db.conn.Exec(
		"INSERT INTO messages (id, from_user_id, to_chat_id, content, reply_to_id) VALUES (?, ?, ?, ?, ?)",
		msgID.String(), fromUserID.String(), toChatID, content, replyIDStr,
	)
	return err
}

// GetChatHistory получает историю чата (последние 50 сообщений)
func (db *DB) GetChatHistory(chatID string, limit int) ([]map[string]interface{}, error) {
	rows, err := db.conn.Query(`
        SELECT m.id, m.content, m.created_at, u.username as from_username
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
		var id, content, fromUsername string
		var createdAt time.Time

		if err := rows.Scan(&id, &content, &createdAt, &fromUsername); err != nil {
			continue
		}

		messages = append(messages, map[string]interface{}{
			"id":            id,
			"content":       content,
			"from_username": fromUsername,
			"created_at":    createdAt,
		})
	}

	return messages, nil
}

// CheckPassword проверяет пароль пользователя
func (db *DB) CheckPassword(username, password string) (*User, error) {
	// Получаем пользователя
	user, err := db.GetUserByUsername(username)
	if err != nil {
		return nil, err
	}

	// Сравниваем пароль с хешем
	err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password))
	if err != nil {
		return nil, err
	}

	return user, nil
}

// Close закрывает соединение с БД
func (db *DB) Close() error {
	return db.conn.Close()
}
