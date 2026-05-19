package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/Montorz/messenger/internal/chat"
	"github.com/google/uuid"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// создаём хаб
	hub := chat.NewHub()
	go hub.Run()

	// WebSocket endpoint (временно без JWT)
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		// Получаем username из query параметра
		username := r.URL.Query().Get("username")
		if username == "" {
			// Если не указан, генерируем случайный
			username = "user_" + uuid.New().String()[:8]
		}

		userID := uuid.New()

		log.Printf("Новое подключение: %s (%s)", username, userID)
		chat.ServeWs(hub, w, r, userID, username)
	})

	// запускаем сервер
	server := &http.Server{Addr: ":8080", Handler: nil}

	// shutdown
	go func() {
		log.Println("Сервер запущен на http://localhost:8080")
		log.Println("WebSocket endpoint: ws://localhost:8080/ws")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("Ошибка сервера:", err)
		}
	}()

	// обработка сигналов
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Выключение сервера...")
	server.Close()
}
