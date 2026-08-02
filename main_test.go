package main

import (
	"bytes"
	"os"
	"testing"
)

// 非交互无参数运行：rootCmd 应输出 help（含全部子命令），而不是进入交互菜单卡住（Linux 无参数场景的脚本/管道安全）
func TestRootNoArgsNonInteractiveShowsHelp(t *testing.T) {
	os.Unsetenv("SSL_ASSISTANT_INTERACTIVE")
	rootCmd.SetArgs([]string{})
	var buf bytes.Buffer
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	err := rootCmd.Execute()
	w.Close()
	os.Stdout = old
	buf.ReadFrom(r)
	if err != nil {
		t.Fatalf("Execute 失败: %v", err)
	}
	out := buf.String()
	for _, cmd := range []string{"init", "add", "del", "show", "update", "find", "cron", "version", "checkupdate"} {
		if !bytes.Contains([]byte(out), []byte(cmd)) {
			t.Fatalf("help 缺少子命令 %s:\n%s", cmd, out)
		}
	}
}
