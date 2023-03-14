package conf

import (
	"io/ioutil"
	"os"
	"sync"

	"gopkg.in/yaml.v3"
	"maitian.com/kepler/rtclib/logger"
)

type Config struct {
	Id         string
	Name       string
	RoomId     string
	SignalIp   string
	SignalPort string
}

var gConfig *Config
var once sync.Once

func GetConfig() *Config {
	return gConfig
}

func init() {
	once.Do(func() {
		Load()
	})
}

func Load() error {
	dir, _ := os.Getwd()
	var data []byte
	var err error

	logger.Infof("basedir: %s", dir)
	paths := make([]string, 0)
	paths = append(paths, dir+"/answer.yml")

	for _, path := range paths {
		data, err = ioutil.ReadFile(path)
		if err == nil {
			break
		}
	}

	if err != nil {
		logger.Info(err.Error())
		return err
	}

	err = yaml.Unmarshal(data, &gConfig)
	if err != nil {
		logger.Info(err.Error())
		return err
	}

	logger.Infof("cfg: %+v", gConfig)

	return nil
}
