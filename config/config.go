package config

import (
	"fmt"
	"github.com/go-ini/ini"
	"os"
)

var (
	configPath string = "config/conf.ini"
	config     *ini.File
)

// InitConfig 初始化配置文件，若文件不存在则创建
func InitConfig() error {
	// 检查文件是否存在
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// 创建目录
		if err := os.MkdirAll("config", 0755); err != nil {
			return fmt.Errorf("创建配置目录失败: %w", err)
		}
		// 创建空的配置文件
		file, err := os.Create(configPath)
		if err != nil {
			return fmt.Errorf("创建配置文件失败: %w", err)
		}
		file.Close()
	}

	// 加载配置文件
	var err error
	config, err = ini.Load(configPath)
	if err != nil {
		return fmt.Errorf("加载配置文件失败: %w", err)
	}
	return nil
}

func GetConfig(rootName string, keyName string) (string, error) {
	if config == nil {
		return "", fmt.Errorf("配置文件未初始化，请先调用 InitConfig")
	}
	section := config.Section(rootName).Key(keyName).String()
	return section, nil
}

func SetConfig(rootName string, keyName string, value string) error {
	if config == nil {
		return fmt.Errorf("配置文件未初始化，请先调用 InitConfig")
	}
	config.Section(rootName).Key(keyName).SetValue(value)
	err := config.SaveTo(configPath)
	if err != nil {
		panic(err)
	}
	return err
}

// GetConfigs 批量获取指定根节点下的配置项
func GetConfigs() (map[string]string, error) {
	if config == nil {
		return nil, fmt.Errorf("配置文件未初始化，请先调用 InitConfig")
	}

	// 获取所有 Section（包括默认 Section）
	sections := config.Sections()
	configs := make(map[string]string)
	for _, section := range sections {
		keyField := ""
		// 获取当前 Section 下的所有 Key
		keys := section.Keys()
		for _, key := range keys {
			if section.Name() == "DEFAULT" {
				keyField = key.Name()
			} else {
				keyField = fmt.Sprintf("%s.%s", section.Name(), key.Name())
			}
			configs[keyField] = key.Value()
		}
	}
	return configs, nil
}

func GetThirdCofig(third string, keyName string) (string, error) {
	if config == nil {
		return "", fmt.Errorf("配置文件未初始化，请先调用 InitConfig")
	}
	rootName := "third." + third
	section := config.Section(rootName).Key(keyName).String()
	return section, nil
}
