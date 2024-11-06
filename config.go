package main

import (
	"gopkg.in/ini.v1"
)

const configFilePath = "config.ini" // 配置文件路径

var curPath string

func loadConfig() error {
	cfg, err := ini.Load(configFilePath)
	if err != nil {
		return err
	}
	curPath = cfg.Section("").Key("curPath").String()
	return nil
}

func updateConfig(newPath string) error {
	cfg, err := ini.Load(configFilePath)
	if err != nil {
		return err
	}
	cfg.Section("").Key("curPath").SetValue(newPath)
	return cfg.SaveTo(configFilePath)
}
