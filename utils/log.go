package utils

import (
	"fmt"
	"log"
	"os"
)

// SaveLog 写日志
func SaveLog(logText string, fileName string, prefix string) {
	logFile, err := os.OpenFile(fileName, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Println("open log file failed, err:", err)
		return
	}
	// 保存原始标准输出
	oldStdout := os.Stdout
	// 将标准输出重定向到文件
	os.Stdout = logFile
	log.SetOutput(logFile)
	log.SetFlags(log.Llongfile | log.Lmicroseconds | log.Ldate)
	log.Println(logText)
	log.SetPrefix(prefix)
	// 恢复标准输出
	os.Stdout = oldStdout
}
