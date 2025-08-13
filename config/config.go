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

// Entry 定义配置项结构体，用于保存键值对并保留顺序
type Entry struct {
	Key   string // 配置键（格式："section.key" 或 "key"）
	Value string // 配置值
}

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
	cfg, err := ini.Load(configPath)
	if err != nil {
		return "", fmt.Errorf("加载配置文件失败: %w", err)
	}
	section := cfg.Section(rootName).Key(keyName).String()
	return section, nil
}

func SetConfig(rootName string, keyName string, value string) error {
	cfg, err := ini.Load(configPath)
	if err != nil {
		return fmt.Errorf("加载配置文件失败: %w", err)
	}
	cfg.Section(rootName).Key(keyName).SetValue(value)
	err = cfg.SaveTo(configPath)
	if err != nil {
		panic(err)
	}
	return err
}

// GetConfigs 批量获取指定根节点下的配置项
func GetConfigs() ([]Entry, error) {
	cfg, err := ini.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("加载配置文件失败: %w", err)
	}
	// 获取所有 Section（包括默认 Section）
	sections := cfg.Sections()
	var configs []Entry // 使用切片替代 map 以保留顺序

	for _, section := range sections {
		keyField := ""
		// 获取当前 Section 下的所有 Key（按 INI 文件中出现顺序）
		keys := section.Keys()
		for _, key := range keys {
			// 构建键名（DEFAULT  section 直接用 key 名，其他 section 格式为 "section.key"）
			if section.Name() == "DEFAULT" {
				keyField = key.Name()
			} else {
				keyField = fmt.Sprintf("%s.%s", section.Name(), key.Name())
			}
			// 按顺序添加到切片
			configs = append(configs, Entry{
				Key:   keyField,
				Value: key.Value(),
			})
		}
	}
	return configs, nil
}

func GetThirdCofig(third string, keyName string) (string, error) {
	cfg, err := ini.Load(configPath)
	if err != nil {
		return "", fmt.Errorf("加载配置文件失败: %w", err)
	}
	rootName := "third." + third
	section := cfg.Section(rootName).Key(keyName).String()
	return section, nil
}
