package config

import (
	"fmt"
	"github.com/go-ini/ini"
	"os"
	"sync"
)

var (
	configPath string = "config/conf.ini"
	config     *ini.File
	mu         sync.RWMutex
)

// Entry 定义配置项结构体，用于保存键值对并保留顺序
type Entry struct {
	Key   string // 配置键（格式："section.key" 或 "key"）
	Value string // 配置值
}

// InitConfig 初始化配置文件，若文件不存在则创建（进程内仅需调用一次，后续 Get/Set 走内存缓存）
func InitConfig() error {
	mu.Lock()
	defer mu.Unlock()
	return initConfigLocked()
}

func initConfigLocked() error {
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

	// 加载配置文件到内存缓存
	var err error
	config, err = ini.Load(configPath)
	if err != nil {
		return fmt.Errorf("加载配置文件失败: %w", err)
	}
	return nil
}

// ensureLoaded 确保缓存已加载（double-checked locking，调用方不得持有任何锁）
func ensureLoaded() error {
	if config != nil {
		return nil
	}
	mu.Lock()
	defer mu.Unlock()
	if config != nil {
		return nil
	}
	return initConfigLocked()
}

// loadedRead 以只读方式访问缓存；若未加载则先确保加载（返回后调用方须释放返回的读锁）
func loadedRead() (release func()) {
	mu.RLock()
	if config != nil {
		return mu.RUnlock
	}
	mu.RUnlock()

	if err := ensureLoaded(); err != nil {
		return nil
	}
	mu.RLock()
	return mu.RUnlock
}

func GetConfig(rootName string, keyName string) (string, error) {
	release := loadedRead()
	if release == nil {
		return "", fmt.Errorf("加载配置文件失败")
	}
	defer release()
	return config.Section(rootName).Key(keyName).String(), nil
}

func SetConfig(rootName string, keyName string, value string) error {
	mu.Lock()
	defer mu.Unlock()
	if config == nil {
		if err := initConfigLocked(); err != nil {
			return err
		}
	}
	config.Section(rootName).Key(keyName).SetValue(value)
	err := config.SaveTo(configPath)
	if err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}
	return nil
}

// GetConfigs 批量获取指定根节点下的配置项
func GetConfigs() ([]Entry, error) {
	release := loadedRead()
	if release == nil {
		return nil, fmt.Errorf("加载配置文件失败")
	}
	defer release()

	// 获取所有 Section（包括默认 Section）
	sections := config.Sections()
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
	release := loadedRead()
	if release == nil {
		return "", fmt.Errorf("加载配置文件失败")
	}
	defer release()
	rootName := "third." + third
	return config.Section(rootName).Key(keyName).String(), nil
}
