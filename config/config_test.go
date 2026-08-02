package config

import (
	"os"
	"strings"
	"testing"
)

// TestMain 切换工作目录到临时目录，避免污染项目 config/conf.ini
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "config_test")
	if err != nil {
		panic(err)
	}
	oldWd, _ := os.Getwd()
	if err := os.Chdir(tmp); err != nil {
		panic(err)
	}
	if err := InitConfig(); err != nil {
		panic(err)
	}

	code := m.Run()

	os.Chdir(oldWd)
	os.RemoveAll(tmp)
	os.Exit(code)
}

func TestSetGetConfig(t *testing.T) {
	// 使用独立键名，避免测试间顺序耦合（-run / -shuffle 均可独立运行）
	key := "unit_test_restart_cmd"
	if err := SetConfig("", key, "nginx -s reload"); err != nil {
		t.Fatalf("SetConfig 失败: %v", err)
	}
	v, err := GetConfig("", key)
	if err != nil {
		t.Fatalf("GetConfig 失败: %v", err)
	}
	if v != "nginx -s reload" {
		t.Fatalf("值不一致: %s", v)
	}

	// 覆盖更新（内存缓存立即生效）
	if err := SetConfig("", key, "nginx -t && nginx -s reload"); err != nil {
		t.Fatal(err)
	}
	v, _ = GetConfig("", key)
	if v != "nginx -t && nginx -s reload" {
		t.Fatalf("覆盖更新失败: %s", v)
	}
}

func TestThirdConfig(t *testing.T) {
	section := "third.unit"
	if err := SetConfig(section, "api_url", "http://test.local"); err != nil {
		t.Fatal(err)
	}
	// GetConfig 与 GetThirdCofig 两种方式访问 third.<name> section
	v1, _ := GetConfig(section, "api_url")
	v2, _ := GetThirdCofig("unit", "api_url")
	if v1 != "http://test.local" || v2 != "http://test.local" {
		t.Fatalf("third 配置读写不一致: %s / %s", v1, v2)
	}
}

func TestGetConfigs(t *testing.T) {
	// 自建数据后断言，不依赖其他测试的写入
	if err := SetConfig("", "unit_test_list_key", "list-value"); err != nil {
		t.Fatal(err)
	}
	if err := SetConfig("third.unit2", "k", "v2"); err != nil {
		t.Fatal(err)
	}
	configs, err := GetConfigs()
	if err != nil {
		t.Fatal(err)
	}
	keys := make(map[string]string)
	for _, e := range configs {
		keys[e.Key] = e.Value
	}
	if keys["unit_test_list_key"] != "list-value" {
		t.Fatalf("GetConfigs 应包含 DEFAULT 键，实际 keys: %v", keys)
	}
	if keys["third.unit2.k"] != "v2" {
		t.Fatalf("GetConfigs 应包含 third.unit2.k，实际 keys: %v", keys)
	}
}

func TestInitConfigIdempotent(t *testing.T) {
	// 自建数据后断言持久化
	if err := SetConfig("", "unit_test_persist", "persisted-value"); err != nil {
		t.Fatal(err)
	}
	// 重复初始化不应报错（文件已存在）
	if err := InitConfig(); err != nil {
		t.Fatalf("重复 InitConfig 失败: %v", err)
	}
	// 文件应包含此前写入的配置（SetConfig 落盘生效）
	content, err := os.ReadFile("config/conf.ini")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "persisted-value") {
		t.Fatalf("conf.ini 未持久化配置:\n%s", content)
	}
	// 内存缓存仍可读到
	v, _ := GetConfig("", "unit_test_persist")
	if v != "persisted-value" {
		t.Fatalf("重复初始化后读取失败: %s", v)
	}
}
