package storage

import (
	"database/sql"
	"log"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

// User - модель пользователя в системе
type User struct {
	ID        uuid.UUID
	Username  string
	Password  string
	CreatedAt time.Time
	LastSeen  time.Time
}

// Room - модель комнаты (группового чата)
type Room struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

// DB - обёртка над sql.DB
type DB struct {
	conn *sql.DB // Подключение к БД
}

// NewDB - создаёт новое подключение к БД и инициализирует таблицы
func NewDB(dbPath string) (*DB, error) {
	// Открываем подключение к SQLite
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	// Проверяем, что БД доступна
	if err := conn.Ping(); err != nil {
		return nil, err
	}

	// Создаём таблицы (если их нет)
	if err := createTables(conn); err != nil {
		return nil, err
	}

	log.Println("Подключение к SQLite установлено")
	return &DB{conn: conn}, nil
}

// createTables - создаёт все необходимые таблицы в БД
func createTables(conn *sql.DB) error {
	queries := []string{
		// Таблица пользователей
		`CREATE TABLE IF NOT EXISTS users (
            id TEXT PRIMARY KEY,                      -- UUID пользователя
            username TEXT UNIQUE NOT NULL,            -- Уникальное имя
            password TEXT NOT NULL,                   -- Хеш пароля (bcrypt)
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            last_seen DATETIME DEFAULT CURRENT_TIMESTAMP
        )`,
		// Таблица комнат
		`CREATE TABLE IF NOT EXISTS rooms (
            id TEXT PRIMARY KEY,                      -- "room:gamers"
            name TEXT UNIQUE NOT NULL,                -- Название комнаты
            created_by TEXT NOT NULL,                 -- Кто создал
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (created_by) REFERENCES users(username)
        )`,
		// Таблица участников комнат
		`CREATE TABLE IF NOT EXISTS room_members (
            room_id TEXT NOT NULL,                    -- ID комнаты
            username TEXT NOT NULL,                   -- Имя пользователя
            joined_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            PRIMARY KEY (room_id, username),          -- Составной ключ
            FOREIGN KEY (room_id) REFERENCES rooms(id),
            FOREIGN KEY (username) REFERENCES users(username)
        )`,
		// Таблица сообщений
		`CREATE TABLE IF NOT EXISTS messages (
            id TEXT PRIMARY KEY,                      -- UUID сообщения
            from_user_id TEXT NOT NULL,               -- UUID отправителя
            to_chat_id TEXT NOT NULL,                 -- "user:alice" или "room:gamers"
            content TEXT NOT NULL,                    -- Текст сообщения
            reply_to_id TEXT,                         -- ID сообщения (для тредов)
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (from_user_id) REFERENCES users(id),
            FOREIGN KEY (reply_to_id) REFERENCES messages(id)
        )`,
		// Индексы для оптимизации запросов
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

// CreateUser - создаёт нового пользователя в БД
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

	// Вставка в БД
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

// GetUserByUsername - получает пользователя по имени
func (db *DB) GetUserByUsername(username string) (*User, error) {
	var user User
	var idStr string // Для парсинга UUID

	err := db.conn.QueryRow(
		"SELECT id, username, password, created_at, last_seen FROM users WHERE username = ?",
		username,
	).Scan(&idStr, &user.Username, &user.Password, &user.CreatedAt, &user.LastSeen)

	if err != nil {
		return nil, err
	}

	// Парсим UUID из строки
	user.ID, err = uuid.Parse(idStr)
	return &user, err
}

// CheckPassword - проверяет пароль пользователя
func (db *DB) CheckPassword(username, password string) (*User, error) {
	user, err := db.GetUserByUsername(username)
	if err != nil {
		return nil, err
	}

	// Сравниваем хеш пароля из БД с введённым паролем
	err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password))
	if err != nil {
		return nil, err
	}

	return user, nil
}

// UpdateLastSeen - обновляет время последней активности пользователя
func (db *DB) UpdateLastSeen(userID uuid.UUID) error {
	_, err := db.conn.Exec(
		"UPDATE users SET last_seen = CURRENT_TIMESTAMP WHERE id = ?",
		userID.String(),
	)
	return err
}

// SaveMessage - сохраняет сообщение в БД
func (db *DB) SaveMessage(fromUserID uuid.UUID, toChatID, content string, replyToID *uuid.UUID) error {
	id := uuid.New() // Новый UUID для сообщения

	// Преобразуем *uuid.UUID в interface{} для SQL
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

	log.Printf("Сохранено сообщение: from=%s, to=%s, content=%s",
		fromUserID.String()[:8], toChatID, content)
	return nil
}

// GetChatHistory - получает историю сообщений для чата (комнаты или личного)
func (db *DB) GetChatHistory(chatID string, limit int) ([]map[string]interface{}, error) {
	rows, err := db.conn.Query(`
        SELECT m.id, m.content, m.created_at, u.username as from_username, m.from_user_id
        FROM messages m
        JOIN users u ON m.from_user_id = u.id
        WHERE m.to_chat_id = ?
        ORDER BY m.created_at ASC
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

		messages = append(messages, map[string]interface{}{
			"id":            id,
			"content":       content,
			"from_username": fromUsername,
			"from_user_id":  fromUserID,
			"created_at":    createdAt,
		})
	}

	return messages, nil
}

// GetPrivateChatHistory - получает историю переписки между двумя пользователями
func (db *DB) GetPrivateChatHistory(user1, user2 string, limit int) ([]map[string]interface{}, error) {
	// Находим ID пользователей
	var user1ID, user2ID string
	err := db.conn.QueryRow("SELECT id FROM users WHERE username = ?", user1).Scan(&user1ID)
	if err != nil {
		return nil, err
	}
	err = db.conn.QueryRow("SELECT id FROM users WHERE username = ?", user2).Scan(&user2ID)
	if err != nil {
		return nil, err
	}

	// Формируем ID чатов
	chatID1 := "user:" + user2 // от user1 к user2
	chatID2 := "user:" + user1 // от user2 к user1

	rows, err := db.conn.Query(`
        SELECT m.id, m.content, m.created_at, u.username as from_username
        FROM messages m
        JOIN users u ON m.from_user_id = u.id
        WHERE (m.to_chat_id = ? AND m.from_user_id = ?)
           OR (m.to_chat_id = ? AND m.from_user_id = ?)
        ORDER BY m.created_at ASC
        LIMIT ?
    `, chatID1, user1ID, chatID2, user2ID, limit)

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

// CreateRoom - создаёт новую комнату
func (db *DB) CreateRoom(roomName, createdBy string) error {
	roomID := "room:" + roomName

	// Создаём комнату
	_, err := db.conn.Exec(
		"INSERT INTO rooms (id, name, created_by) VALUES (?, ?, ?)",
		roomID, roomName, createdBy,
	)
	if err != nil {
		return err
	}

	// Добавляем создателя в участники
	_, err = db.conn.Exec(
		"INSERT INTO room_members (room_id, username) VALUES (?, ?)",
		roomID, createdBy,
	)
	return err
}

// JoinRoom - добавляет пользователя в комнату
func (db *DB) JoinRoom(roomName, username string) error {
	roomID := "room:" + roomName

	// Проверяем существование комнаты
	var exists bool
	err := db.conn.QueryRow("SELECT EXISTS(SELECT 1 FROM rooms WHERE name = ?)", roomName).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return sql.ErrNoRows
	}

	// Добавляем пользователя (IGNORE если уже есть)
	_, err = db.conn.Exec(
		"INSERT OR IGNORE INTO room_members (room_id, username) VALUES (?, ?)",
		roomID, username,
	)
	return err
}

// LeaveRoom - удаляет пользователя из комнаты
func (db *DB) LeaveRoom(roomName, username string) error {
	roomID := "room:" + roomName

	_, err := db.conn.Exec(
		"DELETE FROM room_members WHERE room_id = ? AND username = ?",
		roomID, username,
	)
	return err
}

// GetAllRooms - возвращает все существующие комнаты
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

// GetUserRooms - возвращает комнаты, в которых состоит пользователь
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

// IsRoomMember - проверяет, состоит ли пользователь в комнате
func (db *DB) IsRoomMember(roomName, username string) (bool, error) {
	roomID := "room:" + roomName
	var exists bool
	err := db.conn.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM room_members WHERE room_id = ? AND username = ?)",
		roomID, username,
	).Scan(&exists)
	return exists, err
}

// Close - закрывает соединение с БД вызывается при завершении работы сервера (defer db.Close())
func (db *DB) Close() error {
	return db.conn.Close()
}
