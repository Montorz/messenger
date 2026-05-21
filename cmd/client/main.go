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

	url := fmt.Sprintf("ws://localhost:8080/ws?token=%s", token)
	log.Printf("Подключение...")

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		log.Fatal("Ошибка подключения:", err)
	}
	defer conn.Close()

	log.Printf("Добро пожаловать, %s!", username)

	fmt.Println("\nКОМАНДЫ:")
	fmt.Println("  просто текст         - отправить сообщение в текущую комнату")
	fmt.Println("  /create <название>   - создать новую комнату и войти в неё")
	fmt.Println("  /join <название>     - войти в комнату")
	fmt.Println("  /leave <название>    - выйти из комнаты")
	fmt.Println("  /rooms               - список всех доступных комнат")
	fmt.Println("  /room                - показать текущую комнату")
	fmt.Println("  /msg <user> <текст>  - личное сообщение")
	fmt.Println("  /users               - список онлайн")
	fmt.Println("  /exit                - выход")
	fmt.Println()

	fmt.Println("Вы не в комнате. Используйте /join <название> или /create <название>")
	fmt.Print("> ")

	go func() {
		for {
			var msg Message
			if err := conn.ReadJSON(&msg); err != nil {
				log.Printf("\nСоединение разорвано")
				os.Exit(0)
			}

			fmt.Print("\r\033[K")

			if msg.Type == "system" {
				fmt.Printf("%s\n", msg.Content)
			} else if strings.HasPrefix(msg.ToChatID, "user:") {
				fmt.Printf("[ЛИЧНОЕ] от %s: %s\n", msg.FromUsername, msg.Content)
			} else if strings.HasPrefix(msg.ToChatID, "room:") {
				roomName := strings.TrimPrefix(msg.ToChatID, "room:")
				fmt.Printf("[%s] %s: %s\n", roomName, msg.FromUsername, msg.Content)
			}

			fmt.Print("> ")
		}
	}()

	inputScanner := bufio.NewScanner(os.Stdin)
	for inputScanner.Scan() {
		text := strings.TrimSpace(inputScanner.Text())

		if text == "/exit" {
			log.Println("Выход...")
			return
		}

		if text == "/users" {
			conn.WriteJSON(Message{Type: "get_users", ToChatID: "system"})
			fmt.Print("> ")
			continue
		}

		if text == "/rooms" {
			conn.WriteJSON(Message{Type: "list_rooms", ToChatID: "system"})
			fmt.Print("> ")
			continue
		}

		if text == "/room" {
			conn.WriteJSON(Message{Type: "current_room", ToChatID: "system"})
			fmt.Print("> ")
			continue
		}

		if strings.HasPrefix(text, "/create ") {
			roomName := strings.TrimPrefix(text, "/create ")
			conn.WriteJSON(Message{Type: "create_room", ToChatID: "system", Content: roomName})
			fmt.Print("> ")
			continue
		}

		if strings.HasPrefix(text, "/join ") {
			roomName := strings.TrimPrefix(text, "/join ")
			conn.WriteJSON(Message{Type: "join_room", ToChatID: "system", Content: roomName})
			fmt.Print("> ")
			continue
		}

		if strings.HasPrefix(text, "/leave ") {
			roomName := strings.TrimPrefix(text, "/leave ")
			conn.WriteJSON(Message{Type: "leave_room", ToChatID: "system", Content: roomName})
			fmt.Print("> ")
			continue
		}

		if strings.HasPrefix(text, "/msg ") {
			parts := strings.SplitN(text[5:], " ", 2)
			if len(parts) != 2 {
				fmt.Println("Использование: /msg <username> <текст>")
				fmt.Print("> ")
				continue
			}
			conn.WriteJSON(Message{Type: "private", ToChatID: "user:" + parts[0], Content: parts[1]})
			fmt.Print("> ")
			continue
		}

		if text != "" && !strings.HasPrefix(text, "/") {
			// Обычный текст - отправляем в текущую комнату
			conn.WriteJSON(Message{Type: "group", ToChatID: "", Content: text})
			fmt.Print("> ")
		} else if text != "" {
			fmt.Println("Неизвестная команда")
			fmt.Print("> ")
		} else {
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
