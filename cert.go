package main

import (
	"bufio"
	"bytes"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"ssl_assistant/utils"
	"strconv"
	"strings"
	"time"
)

// Config 配置信息结构体
type Config struct {
	ApiUrl     string `json:"ApiUrl"`     // API 地址
	KeyId      string `json:"KeyId"`      // 证书信息获取的凭证
	KeySecret  string `json:"KeySecret"`  // 证书信息获取的凭证
	RestartCmd string `json:"RestartCmd"` // 证书更新后需要执行的命令
	IsInit     bool   `json:"IsInit"`     // 是否已经初始化
}

// ApiResponse API 响应结构体
type ApiResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
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

var defaultReloadCmd string = "nginx -s reload"

// 初始化配置
func initConfig() {
	// 检查是否已经初始化
	isInit := checkInit()
	if isInit {
		err := getConfigInfo()
		if err != nil {
			color.Red("%s", err)
			return
		}
		fmt.Print("程序已经初始化，是否重新初始化？(y/n): ")
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input != "y" && input != "Y" {
			return
		}
	}

	reader := bufio.NewReader(os.Stdin)
	var ApiUrl string
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

	// 输入KeyId
	fmt.Print("请输入 KeyId: ")
	reader = bufio.NewReader(os.Stdin)
	KeyId, _ := reader.ReadString('\n')
	KeyId = strings.TrimSpace(KeyId)

	// 输入KeyId
	fmt.Print("请输入 KeySecret: ")
	reader = bufio.NewReader(os.Stdin)
	KeySecret, _ := reader.ReadString('\n')
	KeySecret = strings.TrimSpace(KeySecret)

	// 输入重载命令
	fmt.Printf("请输入重载命令(如: %s): ", defaultReloadCmd)
	restartCmd, _ := reader.ReadString('\n')
	restartCmd = strings.TrimSpace(restartCmd)
	if restartCmd == "" {
		restartCmd = defaultReloadCmd
	}

	// 保存配置
	err := dbInterface.SaveConfig("ApiUrl", ApiUrl)
	if err != nil {
		fmt.Println("保存 ApiUrl 失败:", err)
		return
	}

	err = dbInterface.SaveConfig("KeyId", KeyId)
	if err != nil {
		fmt.Println("保存 KeyId 失败:", err)
		return
	}

	err = dbInterface.SaveConfig("KeySecret", KeySecret)
	if err != nil {
		fmt.Println("保存 KeySecret 失败:", err)
		return
	}

	err = dbInterface.SaveConfig("restartCmd", restartCmd)
	if err != nil {
		fmt.Println("保存重载命令失败:", err)
		return
	}

	err = dbInterface.SaveConfig("IsInit", "true")
	if err != nil {
		fmt.Println("保存初始化状态失败:", err)
		return
	}

	color.Green("初始化成功")

	// 寻找 Nginx 配置文件
	findNginxConfigs()
}

// 寻找 Nginx 配置文件
func findNginxConfigs() {
	// 常见的 Nginx 配置文件路径
	paths := []string{
		"/www/server/panel/vhost/nginx/*.conf",                             // 宝塔
		"D:\\phpstudy_pro\\Extensions\\Nginx1.15.11\\conf\\vhosts\\*.conf", // 小皮（Windows）
		"/etc/nginx/nginx.conf",
		"/etc/nginx/conf.d/*.conf",
		"/usr/local/nginx/conf/nginx.conf",
		"/usr/local/etc/nginx/nginx.conf",
		"C:\\nginx\\conf\\nginx.conf",
		"D:\\nginx\\conf\\nginx.conf",
	}

	color.Cyan("正在寻找 Nginx 配置文件...")

	for _, path := range paths {
		// 如果路径包含通配符，则使用 Glob 函数
		if strings.Contains(path, "*") {
			matches, err := filepath.Glob(path)
			if err != nil {
				continue
			}

			for _, match := range matches {
				parseNginxConfig(match)
			}
		} else {
			// 否则直接检查文件是否存在
			if _, err := os.Stat(path); err == nil {
				parseNginxConfig(path)
			}
		}
	}
}

// 解析 Nginx 配置文件
func parseNginxConfig(path string) {
	fmt.Println("解析配置文件:", path)

	// 读取配置文件
	content, err := os.ReadFile(path)
	if err != nil {
		fmt.Println("读取配置文件失败:", err)
		return
	}

	// 使用正则表达式匹配 server_name 和 ssl_certificate
	serverNameRegex := regexp.MustCompile(`server_name\s+([^;]+);`)
	sslCertRegex := regexp.MustCompile(`ssl_certificate\s+([^;]+);`)
	sslKeyRegex := regexp.MustCompile(`ssl_certificate_key\s+([^;]+);`)

	serverNameMatches := serverNameRegex.FindAllStringSubmatch(string(content), -1)
	sslCertMatches := sslCertRegex.FindAllStringSubmatch(string(content), -1)
	sslKeyMatches := sslKeyRegex.FindAllStringSubmatch(string(content), -1)

	// 如果找到了 server_name 和 ssl_certificate，则添加证书
	if len(serverNameMatches) > 0 && len(sslCertMatches) > 0 && len(sslKeyMatches) > 0 {
		for i := 0; i < len(serverNameMatches) && i < len(sslCertMatches) && i < len(sslKeyMatches); i++ {
			serverName := strings.TrimSpace(serverNameMatches[i][1])
			sslCert := strings.TrimSpace(sslCertMatches[i][1])
			sslKey := strings.TrimSpace(sslKeyMatches[i][1])

			// 分割 server_name，可能有多个域名
			domains := strings.Fields(serverName)
			if len(domains) > 0 {
				domain := domains[0]
				color.Cyan("找到域名: %s, 证书: %s, 私钥: %s\n", domain, sslCert, sslKey)

				// 获取证书信息
				cert, err := getCertificateInfo(domain)
				if err != nil {
					fmt.Printf("获取域名 %s 的证书信息失败: %v\n", domain, err)
					continue
				}

				// 设置证书路径
				cert.CertPath = sslCert
				cert.KeyPath = sslKey

				// 保存证书信息
				err = dbInterface.AddCertificate(cert)
				if err != nil {
					fmt.Printf("保存域名 %s 的证书信息失败: %v\n", domain, err)
					continue
				}

				color.Green("域名 %s 的证书信息已保存\n", domain)
			}
		}
	}
}

// 获取证书信息
func getCertificateInfo(domain string) (Certificate, error) {
	var cert Certificate

	configs, err := dbInterface.GetConfigs([]string{"KeyId", "KeySecret", "restartCmd", "ApiUrl"})
	if err != nil {
		return cert, fmt.Errorf("获取配置失败: %v", err)
	}
	//计算token
	token := utils.GetEncodeToken(configs["KeyId"], configs["KeySecret"])

	var ApiPostDataJson = []byte(`{
		"domains": "` + domain + `"
	}`)
	// 调用 API 获取证书信息
	url := fmt.Sprintf("%s/api/v1/cert/get", configs["ApiUrl"]) // 替换为实际的 API 地址
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(ApiPostDataJson))
	if err != nil {
		return cert, fmt.Errorf("创建请求失败: %v", err)
	}

	// 设置请求头
	req.Header.Set("x-certd-token", token)
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")

	// 发送请求
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return cert, fmt.Errorf("发送请求失败: %v", err)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return cert, fmt.Errorf("读取响应失败: %v", err)
	}

	// 解析响应
	var apiResp ApiResponse
	err = json.Unmarshal(body, &apiResp)
	if err != nil {
		return cert, fmt.Errorf("解析响应失败: %v", err)
	}

	// 检查响应状态
	if apiResp.Code != 0 {
		return cert, fmt.Errorf("API 返回错误: %s", apiResp.Message)
	}

	//解析ssl pem证书
	endCertBytes := apiResp.Data.Crt
	endBlocks, _ := pem.Decode([]byte(endCertBytes))
	if endBlocks == nil {
		panic("failed to parse certificate PEM")
	}

	endCert, err := x509.ParseCertificate(endBlocks.Bytes)
	if err != nil {
		panic(err)
	}

	//TODO 如果云端证书与本地证书序列号一致，则不重新下载证书

	fmt.Println("\n=============== 证书信息 start cert ===============")
	fmt.Printf("组织(O): %s %s\n", endCert.Issuer.Organization[0], endCert.Issuer.CommonName)
	fmt.Println("通用名称(CN): ", endCert.Subject.CommonName)
	fmt.Println("证书生效时间: ", endCert.NotBefore.UTC().Format(time.DateTime))
	fmt.Println("证书过期时间: ", endCert.NotAfter.UTC().Format(time.DateTime))
	fmt.Println("签名算法: ", endCert.SignatureAlgorithm)
	fmt.Println("密钥算法: ", endCert.PublicKeyAlgorithm)
	fmt.Println("序列号: ", endCert.SerialNumber)
	if len(endCert.DNSNames) > 0 {
		fmt.Printf("DNS Names: %s\n", utils.ArrayToString(endCert.DNSNames, ","))
	}
	fmt.Printf("=============== 证书信息 end cert ===============\n\n")

	// 设置证书信息
	cert.Domain = domain
	cert.CreateTime = endCert.NotBefore.UTC().Unix()
	cert.ExpireTime = endCert.NotAfter.UTC().Unix()
	if cert.ExpireTime < time.Now().Unix() {
		cert.Status = "过期"
	} else {
		cert.Status = "有效"
	}
	cert.PublicKey = apiResp.Data.Oc
	cert.PrivateKey = apiResp.Data.Key

	return cert, nil
}

// 添加证书
func addCertificate() error {
	initGuide()
	// 输入域名
	fmt.Print("请输入域名: ")
	reader := bufio.NewReader(os.Stdin)
	domain, _ := reader.ReadString('\n')
	domain = strings.TrimSpace(domain)

	// 获取证书信息
	cert, err := getCertificateInfo(domain)
	if err != nil {
		return fmt.Errorf("获取证书信息失败: %s", err)
	}

	// 输入证书路径
	fmt.Print("请输入证书存放路径（需包含文件名）: ")
	certPath, _ := reader.ReadString('\n')
	certPath = strings.TrimSpace(certPath)
	cert.CertPath = certPath

	// 输入私钥路径
	fmt.Print("请输入私钥存放路径（需包含文件名）: ")
	keyPath, _ := reader.ReadString('\n')
	keyPath = strings.TrimSpace(keyPath)
	cert.KeyPath = keyPath

	// 保存证书信息
	err = dbInterface.AddCertificate(cert)
	if err != nil {
		return fmt.Errorf("保存证书信息失败: %s", err)
	}

	color.Green("添加证书成功")

	// 更新证书文件
	err = updateCertificateFiles(cert)
	if err != nil {
		return err
	}
	return err
}

// 删除证书
func deleteCertificate() error {
	initGuide()
	// 输入证书 ID
	fmt.Print("请输入证书 ID: ")
	reader := bufio.NewReader(os.Stdin)
	idStr, _ := reader.ReadString('\n')
	idStr = strings.TrimSpace(idStr)

	// 转换为整数
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return fmt.Errorf("证书 ID 必须是整数")
	}

	// 删除证书
	err = dbInterface.DeleteCertificate(id)
	if err != nil {
		if err.Error() == "Key not found" {
			return fmt.Errorf("证书%s不存在", idStr)
		}
		return fmt.Errorf("删除证书失败: %s", err)
	}

	color.Green("删除证书成功")
	return err
}

// 获取证书并渲染表格
func getCertificates() {
	// 获取所有证书
	certs, err := dbInterface.GetAllCertificates()
	if err != nil {
		fmt.Println("获取证书信息失败:", err)
		return
	}

	// 显示证书信息表格
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"ID", "域名", "状态", "创建时间", "过期时间", "公钥", "私钥"})
	for _, cert := range certs {
		table.Append([]string{
			strconv.Itoa(cert.ID),
			cert.Domain,
			cert.Status,
			time.Unix(cert.CreateTime, 0).Format(time.DateTime),
			time.Unix(cert.ExpireTime, 0).Format(time.DateTime),
			cert.CertPath,
			cert.KeyPath,
		})
	}
	table.Render()
}

// 查看证书
func showCertificates() error {
	initGuide()
	// 处理用户输入
	scanner := bufio.NewScanner(os.Stdin)

	for {
		getCertificates()
		fmt.Println("请输入操作：1=添加、2=删除、3=修改密钥、4=修改重载命令、5=更新证书、9=查看配置信息、0=退出")
		fmt.Print(">>> ")
		if scanner.Scan() {
			input := scanner.Text()
			switch input {
			case "0": // 退出
				return fmt.Errorf("程序退出")
			case "1": // 添加证书
				err := addCertificate()
				if err != nil {
					return err
				}
				continue
			case "2": // 删除证书
				err := deleteCertificate()
				if err != nil {
					return err
				}
				continue
			case "3": // 修改密钥
				err := modifyKey()
				if err != nil {
					return err
				}
				continue
			case "4": // 修改重载命令
				err := modifyRestartCmd()
				if err != nil {
					return err
				}
				continue
			case "5": // 更新证书
				err := updateCertificates()
				if err != nil {
					return err
				}
				continue
			case "9": // 获取配置（测试）
				err := getConfigInfo()
				if err != nil {
					return err
				}
			default:
				fmt.Println("无效的输入，请重新输入")
				continue
			}
		} else {
			fmt.Println("程序退出")
		}
		fmt.Println()
	}
}

// 修改密钥
func modifyKey() error {
	// 输入KeyId
	fmt.Print("请输入 KeyId: ")
	reader := bufio.NewReader(os.Stdin)
	KeyId, _ := reader.ReadString('\n')
	KeyId = strings.TrimSpace(KeyId)

	// 输入KeySecret
	fmt.Print("请输入 KeySecret: ")
	reader = bufio.NewReader(os.Stdin)
	KeySecret, _ := reader.ReadString('\n')
	KeySecret = strings.TrimSpace(KeySecret)

	// 保存 KeyId
	err := dbInterface.SaveConfig("KeyId", KeyId)
	if err != nil {
		return fmt.Errorf("保存 KeyId 失败: %s", err)
	}

	// 保存 KeySecret
	err = dbInterface.SaveConfig("KeySecret", KeySecret)
	if err != nil {
		return fmt.Errorf("保存 KeySecret 失败: %s", err)
	}

	color.Green("修改密钥配置成功")
	return err
}

// 修改重载命令
func modifyRestartCmd() error {
	restartCmd, _ := dbInterface.GetConfig("restartCmd")
	fmt.Printf("当前重载命令: %s\n", color.CyanString(restartCmd))
	fmt.Printf("请输入新的重载命令(如: %s): ", defaultReloadCmd)
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		newCmd := scanner.Text()
		if newCmd == "" {
			newCmd = defaultReloadCmd
		}
		err := dbInterface.SaveConfig("restartCmd", newCmd)
		if err != nil {
			return fmt.Errorf("保存重载命令失败: %s", err)
		} else {
			color.Green("保存重载命令成功")
		}
	}
	return nil
}

// 更新证书
func updateCertificates() error {
	initGuide()
	// 获取所有证书
	certificates, err := dbInterface.GetAllCertificates()
	if err != nil {
		return fmt.Errorf("获取证书信息失败: %s", err)
	}

	// 更新每个证书
	for _, cert := range certificates {
		fmt.Printf("正在更新域名 %s 的证书...\n", cert.Domain)

		// 获取最新的证书信息
		newCert, err := getCertificateInfo(cert.Domain)
		if err != nil {
			color.Red("获取域名 %s 的证书信息失败: %v\n", cert.Domain, err)
			continue
		}

		// 设置证书路径和 ID
		newCert.CertPath = cert.CertPath
		newCert.KeyPath = cert.KeyPath
		newCert.ID = cert.ID

		// 更新证书信息
		err = dbInterface.UpdateCertificate(newCert)
		if err != nil {
			color.Red("更新域名 %s 的证书信息失败: %v\n", cert.Domain, err)
			continue
		}

		// 更新证书文件
		err = updateCertificateFiles(newCert)
		if err != nil {
			return err
		}
	}

	// 执行重载命令
	err = executeRestartCmd()
	if err != nil {
		return err
	}

	fmt.Println("更新证书完成")
	return err
}

// 更新证书文件
func updateCertificateFiles(cert Certificate) error {
	// 提取文件所在的目录
	CertPathDir := filepath.Dir(cert.CertPath)
	KeyPathDir := filepath.Dir(cert.KeyPath)
	//自动创建目录（INFO 或者根本不用考虑自动创建，证书路径不存在本身就说明这个路径是有问题的）
	utils.ExistDir(CertPathDir)
	utils.ExistDir(KeyPathDir)

	// 更新公钥文件
	err := os.WriteFile(cert.CertPath, []byte(cert.PublicKey), 0644)
	if err != nil {
		return fmt.Errorf("更新域名 %s 的公钥文件失败: %v\n", cert.Domain, err)
	}

	// 更新私钥文件
	err = os.WriteFile(cert.KeyPath, []byte(cert.PrivateKey), 0644)
	if err != nil {
		return fmt.Errorf("更新域名 %s 的私钥文件失败: %v\n", cert.Domain, err)
	}

	color.Green("域名 %s 的证书文件已更新\n", cert.Domain)
	return err
}

// 执行重载命令
func executeRestartCmd() error {
	// 获取重载命令
	restartCmd, err := dbInterface.GetConfig("restartCmd")
	if err != nil {
		if err.Error() == "Key not found" {
			return fmt.Errorf("重载命令不存在，请重新初始化")
		}
		return fmt.Errorf("获取重载命令失败: %s", err)
	}

	// 分割命令和参数
	cmdParts := strings.Fields(restartCmd)
	if len(cmdParts) == 0 {
		return fmt.Errorf("重载命令为空")
	}

	// 执行命令
	cmd := exec.Command(cmdParts[0], cmdParts[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("执行重载命令失败: %v\n%s\n", err, output)
	}

	color.Green("执行重载命令成功: %s\n", output)
	return err
}

// 获取配置信息
func getConfigInfo() error {
	config, err := dbInterface.GetConfigs([]string{"ApiUrl", "KeyId", "KeySecret", "restartCmd"})
	if err != nil {
		return fmt.Errorf("获取配置失败: %s", err)
	}
	for key, value := range config {
		if key == "KeySecret" {
			value = "********"
		}
		color.Cyan("%s: %s\n", key, value)
	}
	return err
}

// 检查是否初始化
func checkInit() bool {
	isInit, err := dbInterface.GetConfig("IsInit")
	if err != nil {
		return false
	}
	if isInit == "true" {
		return true
	} else {
		return false
	}
}

// 初始化引导
func initGuide() {
	if !checkInit() {
		color.Red("程序未初始化，现在开始初始化流程")
		initConfig()
	}
}
