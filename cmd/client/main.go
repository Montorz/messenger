package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/gorilla/websocket"
)

func main() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	// подключаемся к серверу
	log.Println("Подключение к ws://localhost:8080/ws...")
	conn, _, err := websocket.DefaultDialer.Dial("ws://localhost:8080/ws", nil)
	if err != nil {
		log.Fatal("Ошибка подключения:", err)
	}
	defer conn.Close()

	log.Println("Подключено!")
	fmt.Println("\nКоманды:")
	fmt.Println("  /msg <текст> - отправить сообщение в группу 'general'")
	fmt.Println("  /exit - выход")
	fmt.Println()

	// горутина для получения сообщений
	go func() {
		for {
			var msg map[string]interface{}
			if err := conn.ReadJSON(&msg); err != nil {
				log.Println("Ошибка чтения:", err)
				return
			}
			fmt.Printf("\n[%s] %s: %s\n", msg["to_chat_id"], msg["from_username"], msg["content"])
			fmt.Print("> ")
		}
	}()

	// отправка сообщений
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		text := scanner.Text()
		if text == "/exit" {
			log.Println("Выход...")
			return
		}

		if strings.HasPrefix(text, "/msg ") {
			content := strings.TrimPrefix(text, "/msg ")
			msg := map[string]interface{}{
				"type":       "group",
				"to_chat_id": "group:general", // теперь отправляем в general
				"content":    content,
			}
			if err := conn.WriteJSON(msg); err != nil {
				log.Println("Ошибка отправки:", err)
			} else {
				log.Println("Сообщение отправлено")
			}
		} else {
			fmt.Println("Неизвестная команда. Используйте /msg <текст> или /exit")
			fmt.Print("> ")
		}
	}
}
