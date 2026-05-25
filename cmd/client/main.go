package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/websocket"
)

// AuthResponse - структура ответа при регистрации или входе
type AuthResponse struct {
	Token string `json:"token"`
	User  struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	} `json:"user"`
}

// Message - структура сообщения для WebSocket (отправка + приём сообщений)
type Message struct {
	Type         string `json:"type"`
	ToChatID     string `json:"to_chat_id"`
	Content      string `json:"content"`
	FromUserID   string `json:"from_user_id,omitempty"`
	FromUsername string `json:"from_username,omitempty"`
}

func main() {
	// Настройка логгера время с точностью до микросекунд
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	fmt.Println("Мессенджер")
	fmt.Println("1. Вход")
	fmt.Println("2. Регистрация")
	fmt.Print("Выберите опцию: ")

	// Создаём сканер для чтения ввода пользователя
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	option := scanner.Text()

	var token, username string

	// Пустая строка = localhost (локальный режим)
	// Любой IP в локальной сети = сетевой режим
	fmt.Print("Введите IP адрес сервера (Enter для localhost): ")
	scanner.Scan()
	serverIP := scanner.Text()
	if serverIP == "" {
		serverIP = "localhost"
	}

	// Формируем URL для API запросов (HTTPS с портом 8443)
	apiURL := fmt.Sprintf("https://%s:8443/api", serverIP)

	if option == "1" {
		fmt.Print("Имя пользователя: ")
		scanner.Scan()
		username = scanner.Text()

		fmt.Print("Пароль: ")
		scanner.Scan()
		password := scanner.Text()

		token = login(apiURL, username, password)
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

		token = register(apiURL, username, password)
		if token == "" {
			log.Fatal("Ошибка регистрации")
		}
	}

	// Настройка WebSocket Dialer для TLS соединения
	websocket.DefaultDialer.TLSClientConfig = &tls.Config{
		InsecureSkipVerify: true, // пропускаем проверку самоподписанного сертификата
	}

	// Подключаемся к WebSocket серверу с полученным JWT токеном
	url := fmt.Sprintf("wss://%s:8443/ws?token=%s", serverIP, token)
	log.Printf("Подключение к %s...", url)

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		log.Fatal("Ошибка подключения:", err)
	}
	defer conn.Close()

	log.Printf("Добро пожаловать, %s!", username)
	log.Printf("Подключение защищено TLS")

	// Вывод справки по командам
	fmt.Println("\nКОМАНДЫ:")
	fmt.Println("  просто текст         - отправить сообщение в текущую комнату")
	fmt.Println("  /create <название>   - создать новую комнату и войти в неё")
	fmt.Println("  /join <название>     - войти в комнату")
	fmt.Println("  /leave <название>    - выйти из комнаты")
	fmt.Println("  /rooms               - список всех доступных комнат")
	fmt.Println("  /room                - показать текущую комнату")
	fmt.Println("  /msg <user> <текст>  - личное сообщение")
	fmt.Println("  /history <username>  - история переписки с пользователем")
	fmt.Println("  /users               - список онлайн")
	fmt.Println("  /exit                - выход")
	fmt.Println()

	fmt.Println("Вы не в комнате. Используйте /join <название> или /create <название>")
	fmt.Print("> ")

	// ПОТОК 1: Асинхронное чтение сообщений от сервера
	go func() {
		for {
			var msg Message

			// Читаем JSON сообщение из WebSocket
			if err := conn.ReadJSON(&msg); err != nil {
				log.Printf("\nСоединение разорвано")
				os.Exit(0)
			}
			fmt.Print("\r\033[K") // Это позволяет перезаписать текущую строку ввода

			// Вывод сообщения в зависимости от его типа
			switch {
			case msg.Type == "system":
				fmt.Printf("%s\n", msg.Content)

			case strings.HasPrefix(msg.ToChatID, "user:"):
				fmt.Printf("[ЛИЧНОЕ] %s: %s\n", msg.FromUsername, msg.Content)

			case strings.HasPrefix(msg.ToChatID, "room:"):
				roomName := strings.TrimPrefix(msg.ToChatID, "room:")
				fmt.Printf("[%s] %s: %s\n", roomName, msg.FromUsername, msg.Content)
			}

			fmt.Print("> ")
		}
	}()

	// ПОТОК 2: Обработка ввода пользователя и отправка команд
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

		// После входа все обычные сообщения идут в эту комнату
		if strings.HasPrefix(text, "/join ") {
			roomName := strings.TrimPrefix(text, "/join ")
			conn.WriteJSON(Message{Type: "join_room", ToChatID: "system", Content: roomName})
			fmt.Print("> ")
			continue
		}

		// После выхода пользователь перестаёт получать сообщения из этой комнаты
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
			conn.WriteJSON(Message{
				Type:     "private",
				ToChatID: "user:" + parts[0],
				Content:  parts[1],
			})
			fmt.Print("> ")
			continue
		}

		// Показывает последние 50 сообщений с указанным пользователем
		if strings.HasPrefix(text, "/history ") {
			targetUsername := strings.TrimPrefix(text, "/history ")
			if targetUsername == "" {
				fmt.Println("Укажите имя пользователя: /history <username>")
				fmt.Print("> ")
				continue
			}
			conn.WriteJSON(Message{
				Type:     "history",
				ToChatID: "system",
				Content:  targetUsername,
			})
			fmt.Print("> ")
			continue
		}

		// Обычное сообщение
		if text != "" && !strings.HasPrefix(text, "/") {
			conn.WriteJSON(Message{Type: "group", ToChatID: "", Content: text})
			fmt.Print("> ")
		} else if text != "" {
			// Неизвестная команда с префиксом /
			fmt.Println("Неизвестная команда")
			fmt.Print("> ")
		} else {
			// Пустая строка - просто показываем приглашение
			fmt.Print("> ")
		}
	}
}

// HTTP метод: POST /api/register
// Тело запроса: {"username": "...", "password": "..."}
// Тело ответа: {"token": "jwt...", "user": {"id": "...", "username": "..."}}
func register(apiURL, username, password string) string {
	// Формируем JSON тело запроса
	data := map[string]string{"username": username, "password": password}
	jsonData, _ := json.Marshal(data)

	// HTTP клиент с игнорированием проверки самоподписанного сертификата
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	// Отправляем POST запрос
	resp, err := httpClient.Post(apiURL+"/register", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Println("Ошибка регистрации:", err)
		return ""
	}
	defer resp.Body.Close()

	// Проверяем статус ответа
	if resp.StatusCode != http.StatusOK {
		log.Println("Ошибка регистрации:", resp.Status)
		return ""
	}

	// Декодируем JSON ответ
	var authResp AuthResponse
	json.NewDecoder(resp.Body).Decode(&authResp)
	log.Printf("Регистрация успешна! Добро пожаловать, %s", username)
	return authResp.Token
}

// HTTP метод: POST /api/login
// Тело запроса: {"username": "...", "password": "..."}
// Тело ответа: {"token": "jwt...", "user": {"id": "...", "username": "..."}}
func login(apiURL, username, password string) string {
	// Формируем JSON тело запроса
	data := map[string]string{"username": username, "password": password}
	jsonData, _ := json.Marshal(data)

	// HTTP клиент с игнорированием проверки самоподписанного сертификата
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	// Отправляем POST запрос
	resp, err := httpClient.Post(apiURL+"/login", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Println("Ошибка входа:", err)
		return ""
	}
	defer resp.Body.Close()

	// Проверяем статус ответа
	if resp.StatusCode != http.StatusOK {
		log.Println("Ошибка входа:", resp.Status)
		return ""
	}

	// Декодируем JSON ответ
	var authResp AuthResponse
	json.NewDecoder(resp.Body).Decode(&authResp)
	log.Printf("Вход выполнен! Добро пожаловать, %s", username)
	return authResp.Token
}
