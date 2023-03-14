package logger

import (
	"fmt"
	"github.com/rs/zerolog"
	"os"
	"sync"
	"time"
)

var log zerolog.Logger
var once sync.Once

type Level uint8

const (
	DebugLevel Level = iota

	InfoLevel

	WarnLevel

	ErrorLevel

	FatalLevel

	PanicLevel

	NoLevel

	Disabled
)

func init() {
	once.Do(func() {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		output := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
		log = zerolog.New(output).With().Timestamp().Logger()
		SetLevel(DebugLevel)
	})
}

func SetLevel(l Level) {
	zerolog.SetGlobalLevel(zerolog.Level(l))
}

func Infof(format string, v ...interface{}) {
	log.Info().Msgf(format, v...)
}

func Info(v ...interface{}) {
	strs := fmt.Sprint(v...)
	log.Info().Msg(strs)
}

func Debugf(format string, v ...interface{}) {
	log.Debug().Msgf(format, v...)
}

func Warnf(format string, v ...interface{}) {
	log.Warn().Msgf(format, v...)
}

func Error(v ...interface{}) {
	strs := fmt.Sprintln(v...)
	log.Error().Msg(strs)
}

func Errorf(format string, v ...interface{}) {
	log.Error().Msgf(format, v...)
}

func Panicf(format string, v ...interface{}) {
	log.Panic().Msgf(format, v...)
}
