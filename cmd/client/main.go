package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Message struct {
	Type         string `json:"type"`
	ToChatID     string `json:"to_chat_id"`
	Content      string `json:"content"`
	FromUserID   string `json:"from_user_id,omitempty"`
	FromUsername string `json:"from_username,omitempty"`
}

func main() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	// Запрашиваем имя пользователя
	fmt.Print("Введите ваше имя: ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	username := scanner.Text()
	if username == "" {
		username = "anon_" + uuid.New().String()[:8]
	}

	// Подключаемся с указанным username
	url := fmt.Sprintf("ws://localhost:8080/ws?username=%s", username)
	log.Printf("Подключение к %s...", url)

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		log.Fatal("Ошибка подключения:", err)
	}
	defer conn.Close()

	log.Printf("Добро пожаловать, %s!", username)

	fmt.Println("\nКоманды:")
	fmt.Println("  /msg <username> <текст> - личное сообщение")
	fmt.Println("  /group <текст>           - сообщение в общий чат")
	fmt.Println("  /users                   - список онлайн")
	fmt.Println("  /exit                    - выход")
	fmt.Println()

	// горутина для получения сообщений
	go func() {
		for {
			var msg Message
			if err := conn.ReadJSON(&msg); err != nil {
				log.Println("Соединение разорвано")
				return
			}

			// Форматируем вывод в зависимости от типа сообщения
			if strings.HasPrefix(msg.ToChatID, "user:") {
				// Личное сообщение
				fmt.Printf("\n[ЛИЧНОЕ] от %s: %s\n", msg.FromUsername, msg.Content)
			} else {
				// Групповое сообщение
				fmt.Printf("\n[%s] %s: %s\n", msg.ToChatID, msg.FromUsername, msg.Content)
			}
			fmt.Print("> ")
		}
	}()

	// отправка сообщений
	scanner = bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		text := scanner.Text()

		if text == "/exit" {
			log.Println("Выход...")
			return
		}

		if text == "/users" {
			// Запрашиваем список пользователей
			msg := Message{
				Type:     "get_users",
				ToChatID: "system",
			}
			conn.WriteJSON(msg)
			continue
		}

		if strings.HasPrefix(text, "/msg ") {
			// Парсим: /msg username привет как дела
			parts := strings.SplitN(text[5:], " ", 2)
			if len(parts) != 2 {
				fmt.Println("Использование: /msg <username> <текст>")
				fmt.Print("> ")
				continue
			}

			username := parts[0]
			content := parts[1]

			msg := Message{
				Type:     "private",
				ToChatID: "user:" + username, // теперь user:username
				Content:  content,
			}
			if err := conn.WriteJSON(msg); err != nil {
				log.Println("Ошибка отправки:", err)
			} else {
				log.Printf("Личное сообщение отправлено пользователю %s", username)
			}
		} else if strings.HasPrefix(text, "/group ") {
			content := strings.TrimPrefix(text, "/group ")
			msg := Message{
				Type:     "group",
				ToChatID: "group:general",
				Content:  content,
			}
			if err := conn.WriteJSON(msg); err != nil {
				log.Println("Ошибка отправки:", err)
			} else {
				log.Println("Сообщение в группу отправлено")
			}
		} else {
			fmt.Println("Неизвестная команда")
			fmt.Println("Доступно: /msg, /group, /users, /exit")
			fmt.Print("> ")
		}
	}
}
