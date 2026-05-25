package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var jwtSecret = []byte("secret-key") // секретный ключ для подписи и верификации JWT токенов

// Claims - структура payload (cодержит идентификационную информацию о пользователе)
type Claims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// GenerateToken - создаёт новый JWT токен для аутентифицированного пользователя
func GenerateToken(userID uuid.UUID, username string) (string, error) {
	// Формируем claims
	claims := Claims{
		UserID:   userID.String(),
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)), // Токен истекает через 24 часа
			IssuedAt:  jwt.NewNumericDate(time.Now()),                     // Время выдачи токена
		},
	}

	// Создаём новый токен с указанным алгоритмом подписи
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// Подписываем токен секретным ключом
	return token.SignedString(jwtSecret)
}

// ValidateToken - проверяет JWT токен и извлекает claims
func ValidateToken(tokenString string) (*Claims, error) {
	// Парсим токен и проверяем подпись
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// Проверяем, что используется ожидаемый алгоритм подписи
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return jwtSecret, nil
	})

	// Ошибка парсинга или проверки подписи
	if err != nil {
		return nil, err
	}

	// Извлекаем claims и проверяем, что токен валидный
	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	// Токен невалидный
	return nil, errors.New("invalid token")
}
