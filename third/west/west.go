package west

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/fatih/color"
	"io"
	"net/http"
	"net/url"
	"os"
	"ssl_assistant/config"
	"ssl_assistant/utils"
	"strconv"
	"strings"
	"time"
)

type auth struct {
	Username string
	Time     int64
	Token    string
}

type ErrorResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

var (
	apiUrl = "https://api.west.cn/newapi/ssl"
)

// SetConfig West配置
func SetConfig() {
	color.Cyan("正在配置West相关参数")
	var reader *bufio.Reader
	var rootName string = "third.west"
	// 输入userName
	fmt.Print("请输入 username（西部数码用户名）: ")
	reader = bufio.NewReader(os.Stdin)
	username, _ := reader.ReadString('\n')
	username = strings.TrimSpace(username)
	err := config.SetConfig(rootName, "username", username)
	if err != nil {
		fmt.Println("保存 username 失败:", err)
		return
	}

	// 输入api_key
	fmt.Print("请输入 apiKey（SSL证书API密钥）: ")
	reader = bufio.NewReader(os.Stdin)
	apiKey, _ := reader.ReadString('\n')
	apiKey = strings.TrimSpace(apiKey)
	err = config.SetConfig(rootName, "api_key", apiKey)
	if err != nil {
		fmt.Println("保存 api_key 失败:", err)
		return
	}

}

// GetCert 获取证书信息 https://console-docs.apipost.cn/preview/4e5d940c9be19cda/73e3028374812fc5?target_id=fae505a9-c375-4e17-a126-656d0b40ba07
func GetCert(domain string) (error, []byte, []byte, []byte) {
	authParam, err := getAuth()
	if err != nil {
		return err, nil, nil, nil
	}
	urlAddress := fmt.Sprintf("%s/info/get-cert?type=PEM_Nginx&domain=%s&%s", apiUrl, domain, authParam)
	resp, err := getRequest(urlAddress)
	if err != nil {
		return fmt.Errorf("请求失败: %s\n", err), nil, nil, nil
	}
	// 1. 检查响应状态码
	if resp.StatusCode != http.StatusOK {
		// 非200状态码通常表示错误
		var errorData ErrorResponse
		err = json.NewDecoder(resp.Body).Decode(&errorData)
		if err != nil {
			return fmt.Errorf("解析错误响应失败: %s\n", err), nil, nil, nil
		}
		return fmt.Errorf("接口错误: %d - %s\n", errorData.Code, errorData.Msg), nil, nil, nil
	}

	// 2. 检查Content-Type头判断响应类型
	contentType := resp.Header.Get("Content-Type")
	switch {
	case strings.Contains(contentType, "application/json"):
		// 处理JSON异常数据
		var errorData ErrorResponse
		err = json.NewDecoder(resp.Body).Decode(&errorData)
		if err != nil {
			return fmt.Errorf("解析JSON错误: %s\n", err), nil, nil, nil
		}
		return fmt.Errorf("接口异常: %d - %s\n", errorData.Code, errorData.Msg), nil, nil, nil
	case strings.Contains(contentType, "application/zip"):
		certDir := "cert"
		utils.ExistDir(certDir)
		fileName := fmt.Sprintf("%s/%s.zip", certDir, domain)
		// 处理ZIP文件流
		file, err := os.Create(fileName)
		if err != nil {
			return fmt.Errorf("创建文件失败: %s\n", err), nil, nil, nil
		}
		defer os.Remove(fileName)
		defer file.Close()

		_, err = io.Copy(file, resp.Body)
		if err != nil {
			return fmt.Errorf("保存文件失败: %s\n", err), nil, nil, nil
		}
		fmt.Println("ZIP文件下载成功")
		err, crt, pem, key := extractCertFiles(fileName)
		if err != nil {
			return fmt.Errorf("证书信息读取失败: %s\n", err), nil, nil, nil
		}
		fmt.Println("证书信息读取成功")
		return err, crt, pem, key
	default:
		// 未知类型
		bodyBytes, _ := io.ReadAll(resp.Body)
		fmt.Printf("未知响应类型: %s\n内容: %s\n", contentType, string(bodyBytes))
	}
	defer resp.Body.Close()

	return nil, nil, nil, nil
}

func getRequest(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	// 添加请求头
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")

	// 发送请求
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, err
}

// 获取鉴权参数
func getAuth() (string, error) {
	username, err := config.GetThirdCofig("west", "username")
	if err != nil {
		return "", fmt.Errorf("获取username配置失败: %s", err)
	}
	apiKey, err := config.GetThirdCofig("west", "api_key")
	if err != nil {
		return "", fmt.Errorf("获取api_key配置失败: %s", err)
	}
	if username == "" || apiKey == "" {
		return "", fmt.Errorf("username或api_key为空，请先配置")
	}
	timestamp := time.Now().Unix()
	token := utils.MD5(username + apiKey + strconv.FormatInt(timestamp, 10))
	c := auth{
		Username: username,
		Time:     timestamp,
		Token:    token,
	}
	// 构建URL参数
	params := url.Values{}
	params.Add("username", c.Username)
	params.Add("time", strconv.FormatInt(c.Time, 10))
	params.Add("token", c.Token)

	// 生成查询字符串
	queryString := params.Encode()
	return queryString, nil
}

// 解析ZIP文件中的证书文件
func extractCertFiles(zipPath string) (error, []byte, []byte, []byte) {
	// 打开ZIP文件
	zipReader, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("打开ZIP文件失败: %v", err), nil, nil, nil
	}
	defer zipReader.Close()

	var pem, key, crt []byte
	// 遍历ZIP中的所有文件
	for _, file := range zipReader.File {
		// 检查文件扩展名
		if isCertFile(file.Name) {
			// 打开ZIP中的文件
			rc, err := file.Open()
			if err != nil {
				return fmt.Errorf("打开ZIP内文件失败: %v", err), nil, nil, nil
			}

			// 读取文件内容
			content, err := io.ReadAll(rc)
			if err != nil {
				rc.Close() // 确保关闭文件
				return fmt.Errorf("读取文件内容失败: %v", err), nil, nil, nil
			}
			rc.Close()

			// 处理文件内容（示例：打印文件名和内容长度）
			debug, err := config.GetConfig("", "debug")
			if debug == "1" {
				fmt.Printf("找到证书文件: %s (大小: %d 字节)\n", file.Name, len(content))
				fmt.Println("证书内容", string(content))
			}

			if hasExtension(file.Name, ".crt") {
				crt = content
			} else if hasExtension(file.Name, ".pem") {
				pem = content
			} else if hasExtension(file.Name, ".key") {
				key = content
			}
		}
	}
	return nil, crt, pem, key
}

// 判断文件是否为PEM或KEY文件
func isCertFile(filename string) bool {
	return hasExtension(filename, ".pem") || hasExtension(filename, ".key") || hasExtension(filename, ".crt")
}

// 检查文件是否有指定的扩展名
func hasExtension(filename, ext string) bool {
	return len(filename) >= len(ext) &&
		filename[len(filename)-len(ext):] == ext
}
