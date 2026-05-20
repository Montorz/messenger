package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// LogLevel определяет уровень логирования
type LogLevel int

const (
	DEBUG LogLevel = 0
	INFO  LogLevel = 1
	WARN  LogLevel = 2
	ERROR LogLevel = 3
)

// Logger структура для логирования
type Logger struct {
	infoLog  *log.Logger
	errorLog *log.Logger
	debugLog *log.Logger
	file     *os.File
	level    LogLevel
}

// NewLogger создаёт новый логгер с записью в файл
func NewLogger(logDir string) (*Logger, error) {
	// Создаём директорию для логов если её нет
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("не удалось создать директорию логов: %v", err)
	}

	// Создаём файл лога с текущей датой
	logFileName := filepath.Join(logDir, fmt.Sprintf("server_%s.log", time.Now().Format("2006-01-02")))

	file, err := os.OpenFile(logFileName, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return nil, fmt.Errorf("не удалось создать файл лога: %v", err)
	}

	// Создаём мульти- writer (в файл и в консоль)
	multiWriter := io.MultiWriter(file, os.Stdout)

	return &Logger{
		infoLog:  log.New(multiWriter, "[INFO] ", log.Ldate|log.Ltime),
		errorLog: log.New(multiWriter, "[ERROR] ", log.Ldate|log.Ltime|log.Lshortfile),
		debugLog: log.New(multiWriter, "[DEBUG] ", log.Ldate|log.Ltime|log.Lshortfile),
		file:     file,
		level:    INFO,
	}, nil
}

// SetLevel устанавливает уровень логирования
func (l *Logger) SetLevel(level LogLevel) {
	l.level = level
}

// Info логирует информационное сообщение
func (l *Logger) Info(format string, v ...interface{}) {
	if l.level <= INFO {
		l.infoLog.Printf(format, v...)
	}
}

// Error логирует ошибку
func (l *Logger) Error(format string, v ...interface{}) {
	if l.level <= ERROR {
		l.errorLog.Printf(format, v...)
	}
}

// Debug логирует отладочное сообщение
func (l *Logger) Debug(format string, v ...interface{}) {
	if l.level <= DEBUG {
		l.debugLog.Printf(format, v...)
	}
}

// Warn логирует предупреждение
func (l *Logger) Warn(format string, v ...interface{}) {
	if l.level <= WARN {
		l.infoLog.Printf("[WARN] "+format, v...)
	}
}

// Close закрывает файл лога
func (l *Logger) Close() error {
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

// LogMessage логирует сообщение пользователя
func (l *Logger) LogMessage(from, to, content string) {
	l.Info("СООБЩЕНИЕ: [%s] -> [%s]: %s", from, to, content)
}

// LogUserAction логирует действие пользователя
func (l *Logger) LogUserAction(username, action string) {
	l.Info("ПОЛЬЗОВАТЕЛЬ: %s - %s", username, action)
}

// LogServerAction логирует действие сервера
func (l *Logger) LogServerAction(action string) {
	l.Info("СЕРВЕР: %s", action)
}

// LogError логирует ошибку с деталями
func (l *Logger) LogError(operation string, err error) {
	l.Error("ОШИБКА: %s - %v", operation, err)
}

// GetCallerInfo возвращает информацию о вызвавшей функции
func GetCallerInfo() string {
	_, file, line, ok := runtime.Caller(2)
	if !ok {
		return "unknown"
	}
	return fmt.Sprintf("%s:%d", filepath.Base(file), line)
}
