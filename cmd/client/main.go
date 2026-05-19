package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/websocket"
)

type AuthResponse struct {
	Token string `json:"token"`
	User  struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	} `json:"user"`
}

type Message struct {
	Type         string `json:"type"`
	ToChatID     string `json:"to_chat_id"`
	Content      string `json:"content"`
	FromUserID   string `json:"from_user_id,omitempty"`
	FromUsername string `json:"from_username,omitempty"`
}

func main() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	fmt.Println("Мессенджер")
	fmt.Println("1. Вход")
	fmt.Println("2. Регистрация")
	fmt.Print("Выберите опцию: ")

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	option := scanner.Text()

	var token, username string

	if option == "1" {
		// Логин
		fmt.Print("Имя пользователя: ")
		scanner.Scan()
		username = scanner.Text()

		fmt.Print("Пароль: ")
		scanner.Scan()
		password := scanner.Text()

		token = login(username, password)
		if token == "" {
			log.Fatal("Ошибка входа")
		}
	} else {
		// Регистрация
		fmt.Print("Имя пользователя: ")
		scanner.Scan()
		username = scanner.Text()

		fmt.Print("Пароль: ")
		scanner.Scan()
		password := scanner.Text()

		token = register(username, password)
		if token == "" {
			log.Fatal("Ошибка регистрации")
		}
	}

	// Подключаемся к WebSocket с токеном
	url := fmt.Sprintf("ws://localhost:8080/ws?token=%s", token)
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

	// Горутина для получения сообщений
	go func() {
		for {
			var msg Message
			if err := conn.ReadJSON(&msg); err != nil {
				log.Println("Соединение разорвано")
				return
			}

			if strings.HasPrefix(msg.ToChatID, "user:") {
				fmt.Printf("\n[ЛИЧНОЕ] от %s: %s\n", msg.FromUsername, msg.Content)
			} else {
				fmt.Printf("\n[%s] %s: %s\n", msg.ToChatID, msg.FromUsername, msg.Content)
			}
			fmt.Print("> ")
		}
	}()

	// Отправка сообщений
	inputScanner := bufio.NewScanner(os.Stdin)
	for inputScanner.Scan() {
		text := inputScanner.Text()

		if text == "/exit" {
			log.Println("Выход...")
			return
		}

		if text == "/users" {
			msg := Message{Type: "get_users", ToChatID: "system"}
			conn.WriteJSON(msg)
			continue
		}

		if strings.HasPrefix(text, "/msg ") {
			parts := strings.SplitN(text[5:], " ", 2)
			if len(parts) != 2 {
				fmt.Println("Использование: /msg <username> <текст>")
				fmt.Print("> ")
				continue
			}

			msg := Message{
				Type:     "private",
				ToChatID: "user:" + parts[0],
				Content:  parts[1],
			}
			conn.WriteJSON(msg)
			log.Printf("Личное сообщение отправлено пользователю %s", parts[0])

		} else if strings.HasPrefix(text, "/group ") {
			content := strings.TrimPrefix(text, "/group ")
			msg := Message{
				Type:     "group",
				ToChatID: "group:general",
				Content:  content,
			}
			conn.WriteJSON(msg)
			log.Println("Сообщение в группу отправлено")

		} else {
			fmt.Println("Неизвестная команда")
			fmt.Print("> ")
		}
	}
}

func register(username, password string) string {
	data := map[string]string{"username": username, "password": password}
	jsonData, _ := json.Marshal(data)

	resp, err := http.Post("http://localhost:8080/api/register", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Println("Ошибка регистрации:", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Println("Ошибка регистрации:", resp.Status)
		return ""
	}

	var authResp AuthResponse
	json.NewDecoder(resp.Body).Decode(&authResp)
	log.Printf("Регистрация успешна! Добро пожаловать, %s", username)
	return authResp.Token
}

func login(username, password string) string {
	data := map[string]string{"username": username, "password": password}
	jsonData, _ := json.Marshal(data)

	resp, err := http.Post("http://localhost:8080/api/login", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Println("Ошибка входа:", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Println("Ошибка входа:", resp.Status)
		return ""
	}

	var authResp AuthResponse
	json.NewDecoder(resp.Body).Decode(&authResp)
	log.Printf("Вход выполнен! Добро пожаловать, %s", username)
	return authResp.Token
}
