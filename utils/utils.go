package utils

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"
)

type encodeJson struct {
	KeyId     string `json:"keyId"`
	TimeStamp int64  `json:"t"`
	Encrypt   bool   `json:"encrypt"`
	SignType  string `json:"signType"`
}

// GetEncodeToken 获取加密token
//
//	@param keyId
//	@param keySecret
//	@return string
func GetEncodeToken(keyId string, keySecret string) string {
	content, _ := json.Marshal(encodeJson{
		KeyId:     keyId,
		Encrypt:   false,
		SignType:  "md5",
		TimeStamp: time.Now().Unix(),
	})
	sign := MD5(fmt.Sprintf("%s%s", content, keySecret))
	return base64.StdEncoding.EncodeToString(content) + "." + base64.StdEncoding.EncodeToString([]byte(sign))
}

// MD5 MD5字符串获取
//
//	@param str
//	@return string
func MD5(str string) string {
	data := []byte(str) //切片
	has := md5.Sum(data)
	md5str := fmt.Sprintf("%x", has) //将[]byte转成16进制
	return md5str
}

// TimeFormat 时间格式化
//
//	@param timeStr
//	@param timeStrLayout
func TimeFormat(timeStr string, timeStrLayout string) (time.Time, error) {
	// 解析时间字符串为 time.Time 对象
	t, err := time.Parse(timeStrLayout, timeStr)
	if err != nil {
		return time.Now(), fmt.Errorf("error parsing time:%s", err)
	}

	// 设置时区为 UTC
	t = t.UTC()

	// 转换为北京时间（UTC+8）
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.Now(), fmt.Errorf("error loading location:%s", err)
	}
	t = t.In(loc)
	return t, err
}

// ExistDir 检查目录是否存在，不存在则创建
//
//	@param path
func ExistDir(path string) {
	// 判断路径是否存在
	_, err := os.ReadDir(path)
	if err != nil {
		// 不存在就创建
		err = os.MkdirAll(path, fs.ModePerm)
		if err != nil {
			fmt.Println(err)
		}
	}
}

// ArrayToString
//
//	@param arr
//	@param suffix
//	@return string
func ArrayToString(arr []string, suffix string) string {
	return strings.Join([]string(arr), suffix)
}
