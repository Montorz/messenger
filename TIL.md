# Дневник разработчика (TIL - Today I Learned)

## Ошибка №1: SQLite на Windows требует CGO

### Описание ошибки
При попытке запустить сервер на Windows возникала ошибка:
Binary was compiled with 'CGO_ENABLED=0', go-sqlite3 requires cgo to work. This is a stub
exit status 1

### Причина
Библиотека `github.com/mattn/go-sqlite3` требует CGO (C Go) и наличия компилятора C на системе. На Windows по умолчанию CGO отключён, а компилятор C не установлен.

### Решение
Заменил драйвер SQLite на `modernc.org/sqlite` - чистую Go-реализацию, не требующую CGO.

### Исправленный код
```go
// Было
import _ "github.com/mattn/go-sqlite3"

// Стало
import _ "modernc.org/sqlite"

// Также изменил имя драйвера с "sqlite3" на "sqlite"
conn, err := sql.Open("sqlite", dbPath)
```

## Ошибка №2: Deadlock (взаимная блокировка) при обработке /users

### Описание ошибки
При вводе команды /users сервер зависал полностью, клиенты переставали отвечать.

### Причина
Вложенные вызовы h.mu.RLock(). Внутри блокировки вызывалась функция findClientByUsername, которая тоже пыталась взять блокировку.

### Исправленный код
```go
// Было (вызывало deadlock)
h.mu.RLock()
targetClient, found := h.findClientByUsername(username) // здесь ещё один RLock
h.mu.RUnlock()

// Стало (собираем данные за один проход)
h.mu.RLock()
var targetClient *Client
for _, client := range h.clients {
    if client.Username == msg.FromUsername {
        targetClient = client
        break
    }
}
h.mu.RUnlock()
```

## Ошибка №3: После /leave сообщения продолжали приходить

### Описание ошибки
Пользователь выходил из комнаты через /leave, но всё равно продолжал получать сообщения из этой комнаты.

### Причина
Пользователь удалялся из таблицы room_members в БД, но оставался в памяти сервера в h.rooms.

### Исправленный код
```go
// Добавил удаление из памяти при выходе из комнаты
h.mu.Lock()
if members, ok := h.rooms[roomID]; ok {
    var clientID uuid.UUID
    for id, client := range h.clients {
        if client.Username == msg.FromUsername {
            clientID = id
            break
        }
    }
    delete(members, clientID)
    
    if len(members) == 0 {
        delete(h.rooms, roomID)
    }
}
h.mu.Unlock()
```

## Ошибка №4: Отправитель видел свои сообщения

### Описание ошибки
Когда пользователь писал сообщение в комнату, он видел его дважды: сразу после отправки и потом от сервера.

### Причина
Сервер отправлял сообщение обратно отправителю при рассылке по комнате.

### Исправленный код
```go
for _, client := range members {
    // Пропускаем отправителя
    if client.Username == msg.FromUsername {
        continue
    }
    client.Send <- msg
}
```

## Ошибка №5: Личные сообщения не работали

### Описание ошибки
При отправке /msg egor привет сообщение не доходило до получателя.

### Причина
Сервер искал получателя по UUID, а клиент передавал username.

### Исправленный код
```go
// Добавил функцию поиска по username
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

// Использование
targetClient, found := h.findClientByUsername(username)
```

## Ошибка №6: История личных сообщений не показывалась

### Описание ошибки
Команда /history egor возвращала "История пуста", хотя сообщения были отправлены.

### Причина
SQL запрос искал сообщения только в одном направлении (от user1 к user2).

### Исправленный код
```sql
-- Было
WHERE m.to_chat_id = ?

-- Стало (ищем в обе стороны)
WHERE (m.to_chat_id = ? AND m.from_user_id = ?)
   OR (m.to_chat_id = ? AND m.from_user_id = ?)
```

## Ошибка №7: UNIQUE constraint failed при сохранении сообщений

### Описание ошибки
При сохранении сообщения в БД возникала ошибка:
UNIQUE constraint failed: messages.id

### Причина
ID сообщения генерировался дважды: один раз при создании сообщения, второй раз при сохранении в БД.

### Исправленный код
```go
// Было - принимали ID из сообщения
func (db *DB) SaveMessage(msgID uuid.UUID, fromUserID uuid.UUID, ...) error

// Стало - генерируем ID внутри функции
func (db *DB) SaveMessage(fromUserID uuid.UUID, toChatID, content string, replyToID *uuid.UUID) error {
    id := uuid.New() // всегда новый ID
    _, err := db.conn.Exec(
        "INSERT INTO messages (id, from_user_id, to_chat_id, content, reply_to_id, created_at) VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)",
        id.String(), fromUserID.String(), toChatID, content, replyIDStr,
    )
    // ...
}
```

## Ошибка №8: Panic при получении истории

### Описание ошибки
Сервер падал с ошибкой:
panic: interface conversion: interface {} is nil, not string

### Причина
В данных из БД некоторые поля могли быть nil, а код пытался привести их к string без проверки.

### Исправленный код
```go
// Добавил проверки типов
for _, msg := range history {
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
    // ... используем данные
}
```

## Ошибка №9: После /history сервер отключался

### Описание ошибки
При вводе /history сервер падал с ошибкой.

### Причина
Функция GetPrivateChatHistory возвращала сообщения без поля created_at.

### Исправленный код
```sql
-- Было (не хватало created_at)
SELECT m.id, m.content, u.username as from_username

-- Стало (добавил created_at)
SELECT m.id, m.content, m.created_at, u.username as from_username
```

## Ошибка №10: Клиент не выводил приглашение после входа

### Описание ошибки
После успешного входа в клиенте не было приглашения к вводу (>).

### Причина
После вывода приветственных сообщений не вызывался fmt.Print("> ").

### Исправленный код
```go
// Добавил приглашение после приветствия
fmt.Println("Добро пожаловать, %s!", username)
fmt.Print("> ")

// В горутине получения сообщений после вывода сообщения
fmt.Print("> ")

// В главном цикле после отправки команды
fmt.Print("> ")
```