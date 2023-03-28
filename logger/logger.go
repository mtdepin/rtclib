package logger

import (
	"fmt"
	"sync"

	"github.com/pion/logging"
)

var loggerFactory logging.LoggerFactory
var log logging.LeveledLogger
var once sync.Once

func init() {
	once.Do(func() {
		loggerFactory = logging.NewDefaultLoggerFactory()
		log = loggerFactory.NewLogger("rtclib")
	})
}

func Infof(format string, v ...interface{}) {
	log.Infof(format, v)
}

func Info(v ...interface{}) {
	strs := fmt.Sprint(v...)
	log.Info(strs)
}

func Debugf(format string, v ...interface{}) {
	log.Debugf(format, v)
}

func Warnf(format string, v ...interface{}) {
	log.Warnf(format, v)
}

func Warn(v ...interface{}) {
	strs := fmt.Sprint(v...)
	log.Warn(strs)
}

func Error(v ...interface{}) {
	strs := fmt.Sprintln(v...)
	log.Error(strs)
}

func Errorf(format string, v ...interface{}) {
	log.Errorf(format, v)
}
