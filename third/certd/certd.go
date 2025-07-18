package certd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/fatih/color"
	"io"
	"net/http"
	"os"
	"ssl_assistant/config"
	"ssl_assistant/utils"
	"strings"
)

// ApiResponse API 响应结构体
type ApiResponse struct {
	Code    int    `json:"code"`
	Message string `json:"msg"`
	Data    struct {
		Crt string `json:"crt"` // 全链证书，fullchain,PEM格式
		Key string `json:"key"` // 私钥，PEM格式
		Ic  string `json:"ic"`  // 中间证书，PEM格式
		Oc  string `json:"oc"`  // 单证书，PEM格式，不含证书链
		Pfx string `json:"pfx"` // PFX格式证书，Base64编码
		Der string `json:"der"` // DER格式证书，Base64编码
		Jks string `json:"jks"` // JKS格式证书，Base64编码
		One string `json:"one"` // 一体化证书，crt+key两个字符串拼接的PEM证书
	} `json:"data"`
}

// GetCertificateInfo 获取证书信息 https://apifox.com/apidoc/shared/2e76f8c4-7c58-413b-a32d-a1316529af44/254949529e0
func GetCertificateInfo(domain string) (error, []byte, []byte, []byte) {
	ApiUrl, err := config.GetConfig("third.certd", "api_url")
	if err != nil {
		return fmt.Errorf("获取api_url配置失败: %v", err), nil, nil, nil
	}
	KeyId, err := config.GetConfig("third.certd", "key_id")
	if err != nil {
		return fmt.Errorf("获取key_id配置失败: %v", err), nil, nil, nil
	}
	KeySecret, err := config.GetConfig("third.certd", "key_secret")
	if err != nil {
		return fmt.Errorf("获取key_secret配置失败: %v", err), nil, nil, nil
	}
	//计算token
	token := utils.GetEncodeToken(KeyId, KeySecret)

	var ApiPostDataJson = []byte(`{
		"domains": "` + domain + `"
	}`)
	// 调用 API 获取证书信息
	url := fmt.Sprintf("%s/api/v1/cert/get", ApiUrl) // 替换为实际的 API 地址
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(ApiPostDataJson))
	if err != nil {
		return fmt.Errorf("创建请求失败: %v", err), nil, nil, nil
	}

	// 设置请求头
	req.Header.Set("x-certd-token", token)
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")

	// 发送请求
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("发送请求失败: %v", err), nil, nil, nil
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %v", err), nil, nil, nil
	}

	// 解析响应
	var apiResp ApiResponse
	err = json.Unmarshal(body, &apiResp)
	if err != nil {
		return fmt.Errorf("解析响应失败: %v", err), nil, nil, nil
	}
	// 检查响应状态
	if apiResp.Code != 0 {
		return fmt.Errorf("API 返回错误: %s", apiResp.Message), nil, nil, nil
	}

	return err, []byte(apiResp.Data.Crt), []byte(apiResp.Data.Ic), []byte(apiResp.Data.Key)
}

// SetConfig Certd配置
func SetConfig() {
	color.Cyan("正在配置Certd相关参数")
	var ApiUrl string
	var reader *bufio.Reader
	var rootName string = "third.certd"
	for {
		// 输入ApiUrl
		fmt.Print("请输入 ApiUrl（例如 http://your-certd-server.com）: ")
		reader = bufio.NewReader(os.Stdin)
		ApiUrl, _ = reader.ReadString('\n')
		ApiUrl = strings.TrimSpace(ApiUrl)
		//判断url是否存在http或者https
		if !strings.HasPrefix(ApiUrl, "http://") && !strings.HasPrefix(ApiUrl, "https://") {
			fmt.Println("ApiUrl 错误，需包含 http:// or https:// 请重新输入")
			continue
		}
		//如果结尾是/则去掉
		ApiUrl = strings.TrimSuffix(ApiUrl, "/")
		break
	}
	err := config.SetConfig(rootName, "api_url", ApiUrl)
	if err != nil {
		fmt.Println("保存 api_url 失败:", err)
		return
	}

	// 输入KeyId
	fmt.Print("请输入 KeyId: ")
	reader = bufio.NewReader(os.Stdin)
	KeyId, _ := reader.ReadString('\n')
	KeyId = strings.TrimSpace(KeyId)
	err = config.SetConfig(rootName, "key_id", KeyId)
	if err != nil {
		fmt.Println("保存 key_id 失败:", err)
		return
	}

	// 输入KeySecret
	fmt.Print("请输入 KeySecret: ")
	reader = bufio.NewReader(os.Stdin)
	KeySecret, _ := reader.ReadString('\n')
	KeySecret = strings.TrimSpace(KeySecret)
	err = config.SetConfig(rootName, "key_secret", KeySecret)
	if err != nil {
		fmt.Println("保存 key_secret 失败:", err)
		return
	}
}
