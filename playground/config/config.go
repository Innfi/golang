package config

import (
	"log"

	"github.com/spf13/viper"
)

type ConfigSet struct {
	Host string
	Port int
}

type ConfigReader struct {
	v *viper.Viper
}

func NewConfigReader() *ConfigReader {
	return &ConfigReader{v: viper.New()}
}

func (c *ConfigReader) Read() *ConfigSet {
	c.v.SetConfigFile("config.yaml")
	if err := c.v.ReadInConfig(); err != nil {
		log.Println("ConfigReader.Read(): ", err)
		return nil
	}

	return &ConfigSet{Host: c.v.GetString("Host"), Port: c.v.GetInt("Port")}
}
