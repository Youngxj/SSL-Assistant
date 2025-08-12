package main

import (
	"bufio"
	"fmt"
	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
	"github.com/robfig/cron/v3"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"ssl_assistant/config"
	"ssl_assistant/db"
	"ssl_assistant/third/certd"
	"ssl_assistant/third/west"
	"ssl_assistant/utils"
	"strconv"
	"strings"
	"time"
)

// 常见的 Nginx 配置文件路径
var defaultNginxPaths = []string{
	"/www/server/panel/vhost/nginx/*.conf", // 宝塔
	"/opt/1panel/www/conf.d/*.conf",        // 1panel
	"/etc/nginx/nginx.conf",
	"/etc/nginx/conf.d/*.conf",
	"/usr/local/nginx/conf/nginx.conf",
	"/usr/local/etc/nginx/nginx.conf",
	"C:\\nginx\\conf\\nginx.conf",
	"D:\\nginx\\conf\\nginx.conf",
}

var defaultReloadCmd string = "nginx -s reload" // 默认重载命令
var defaultBeforeExpirationDay int16 = 10       // 默认证书过期前10天更新

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

	// 设置平台密钥
	modifyKey()

	// 输入重载命令
	fmt.Printf("请输入重载命令(如: %s): ", defaultReloadCmd)
	restartCmd, _ := reader.ReadString('\n')
	restartCmd = strings.TrimSpace(restartCmd)
	if restartCmd == "" {
		restartCmd = defaultReloadCmd
	}

	// 输入提前更新天数
	fmt.Printf("请输入证书提前更新天数(默认: %d天): ", defaultBeforeExpirationDay)
	ExpirationDay, _ := reader.ReadString('\n')
	ExpirationDay = strings.TrimSpace(ExpirationDay)
	if ExpirationDay == "" {
		ExpirationDay = strconv.Itoa(int(defaultBeforeExpirationDay))
	}

	err := config.SetConfig("", "restart_cmd", restartCmd)
	if err != nil {
		fmt.Println("保存重载命令失败:", err)
		return
	}

	err = config.SetConfig("", "before_expiration_day", ExpirationDay)
	if err != nil {
		fmt.Println("保存过期前天数失败:", err)
		return
	}

	err = config.SetConfig("", "is_init", "1")
	if err != nil {
		fmt.Println("保存初始化状态失败:", err)
		return
	}

	color.Green("初始化成功")

	// 寻找 Nginx 配置文件
	findNginxConfigs(defaultNginxPaths)

	color.Yellow("已完成自动检索 Nginx 配置文件，接下来可自定义配置文件路径，如无自定义可跳过")
	// 输入自定义Nginx配置文件路径
	err = findNginxPathCmd()
	if err != nil {
		color.Red("%s", err)
		return
	}

	err = showCertificates()
	if err != nil {
		color.Red("%s", err)
		return
	}
}

// 寻找 Nginx 配置文件
func findNginxConfigs(paths []string) {
	color.Cyan("正在寻找 Nginx 配置文件...")

	for _, path := range paths {
		fmt.Println("正在检索目录: ", path)
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

				if checkHasDomain(domain) {
					color.Yellow("域名 %s 的证书信息已存在，无需重复添加\n", domain)
					continue
				}

				// 获取证书信息
				cert, err := getCertificateInfo(domain, "")
				if err != nil {
					fmt.Printf("获取域名 %s 的证书信息失败: %v\n", domain, err)
					continue
				}
				// 设置证书路径
				cert.CertPath = sslCert
				cert.KeyPath = sslKey

				// 保存证书信息
				err = db.Interface.AddCertificate(cert)
				if err != nil {
					fmt.Printf("保存域名 %s 的证书信息失败: %v\n", domain, err)
					continue
				}

				color.Green("域名 %s 的证书信息已保存\n", domain)
			}
		}
	}
}

// 获取证书信息 certd/west
// @param domain 域名
// @param certSource 证书来源(west/certd)，传空自动判断
// @return db.Certificate 证书信息
func getCertificateInfo(domain string, certSource string) (db.Certificate, error) {
	var cert db.Certificate
	var err error
	var crt, _, key []byte
	switch certSource {
	case "west":
		color.Yellow("正在尝试使用West获取证书信息...\n")
		err, crt, _, key = west.GetCert(domain)
		break
	case "certd":
		color.Yellow("正在尝试使用Certd获取证书信息...\n")
		err, crt, _, key = certd.GetCertificateInfo(domain)
		break
	default:
		color.Yellow("正在尝试使用West获取证书信息...\n")
		err, crt, _, key = west.GetCert(domain)
		if err != nil {
			color.Red("West:%s\n", err)
			color.Yellow("正在尝试使用Certd获取证书信息...\n")
			err, crt, _, key = certd.GetCertificateInfo(domain)
			if err != nil {
				color.Red("Certd:%s\n", err)
				return db.Certificate{}, err
			} else {
				cert.CertSource = "certd"
			}
		} else {
			cert.CertSource = "west"
		}
	}
	if err != nil {
		return db.Certificate{}, err
	}
	endCert := utils.ParseCertificate(crt)
	utils.ShowCertificateInfo(endCert)
	// 设置证书信息
	cert.Domain = domain
	cert.CreateTime = endCert.NotBefore.UTC().Unix()
	cert.ExpireTime = endCert.NotAfter.UTC().Unix()
	if cert.ExpireTime < time.Now().Unix() {
		cert.Status = "过期"
	} else {
		cert.Status = "有效"
	}
	cert.PublicKey = string(crt)
	cert.PrivateKey = string(key)

	return cert, nil
}

// 添加证书
func addCertificate() error {
	initGuide(false)
	// 输入域名
	fmt.Print("请输入域名: ")
	reader := bufio.NewReader(os.Stdin)
	domain, _ := reader.ReadString('\n')
	domain = strings.TrimSpace(domain)

	// 获取证书信息
	cert, err := getCertificateInfo(domain, "")
	if err != nil {
		return fmt.Errorf("获取证书信息失败: %s", err)
	}

	if checkHasDomain(domain) {
		return fmt.Errorf("域名 %s 的证书信息已存在，无需重复添加\n", domain)
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
	err = db.Interface.AddCertificate(cert)
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
	initGuide(false)
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
	err = db.Interface.DeleteCertificate(id)
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
	certs, err := db.Interface.GetAllCertificates()
	if err != nil {
		fmt.Println("获取证书信息失败:", err)
		return
	}

	// 显示证书信息表格
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"ID", "域名", "状态", "创建时间", "过期时间", "剩余天数", "来源", "公钥", "私钥"})
	for _, cert := range certs {
		expireDay := time.Unix(cert.ExpireTime, 0).Sub(time.Now())
		var certStatus string
		if cert.ExpireTime < time.Now().Unix() {
			certStatus = "过期"
		} else {
			certStatus = "有效"
		}

		table.Append([]string{
			strconv.Itoa(cert.ID),
			cert.Domain,
			certStatus,
			time.Unix(cert.CreateTime, 0).Format(time.DateOnly),
			time.Unix(cert.ExpireTime, 0).Format(time.DateOnly),
			strconv.FormatInt(int64(expireDay.Hours()/24), 10),
			cert.CertSource,
			cert.CertPath,
			cert.KeyPath,
		})
	}
	table.Render()
}

// 查看证书
func showCertificates() error {
	initGuide(false)
	// 处理用户输入
	scanner := bufio.NewScanner(os.Stdin)

	for {
		getCertificates()
		fmt.Println("请输入操作：1=添加、2=删除、3=修改密钥、4=修改重载命令、5=更新证书、6=修改提前更新天数、7=快速添加域名（Nginx目录检索）、9=查看配置信息、0=退出")
		fmt.Print(">>> ")
		if scanner.Scan() {
			input := scanner.Text()
			switch input {
			case "0": // 退出
				fmt.Println("程序退出")
				os.Exit(0)
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
			case "3":
				modifyKey()
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
			case "6": // 更新到期检查时间
				err := modifyExpirationDay()
				if err != nil {
					return err
				}
				continue
			case "7": // 快速添加域名
				err := findNginxPathCmd()
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
			os.Exit(0)
		}
		fmt.Println()
	}
}

// 修改重载命令
func modifyRestartCmd() error {
	restartCmd, _ := config.GetConfig("", "restart_cmd")
	fmt.Printf("当前重载命令: %s\n", color.CyanString(restartCmd))
	fmt.Printf("请输入新的重载命令(如: %s): ", defaultReloadCmd)
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		newCmd := scanner.Text()
		if newCmd == "" {
			newCmd = defaultReloadCmd
		}
		err := config.SetConfig("", "restart_cmd", newCmd)
		if err != nil {
			return fmt.Errorf("保存重载命令失败: %s", err)
		} else {
			color.Green("保存重载命令成功")
		}
	}
	return nil
}

// 修改过期前检查天数
func modifyExpirationDay() error {
	ExpirationDay, _ := config.GetConfig("", "before_expiration_day")
	fmt.Printf("当前过期前天数: %s\n", color.CyanString(ExpirationDay))
	fmt.Printf("请输入新的过期前天数(如: %d): ", defaultBeforeExpirationDay)
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		newDay, _ := strconv.Atoi(scanner.Text())
		if newDay == 0 {
			newDay = int(defaultBeforeExpirationDay)
		}
		err := config.SetConfig("", "before_expiration_day", strconv.Itoa(newDay))
		if err != nil {
			return fmt.Errorf("保存过期前天数失败: %s", err)
		} else {
			color.Green("过期前天数已修改成: %s", color.CyanString(strconv.Itoa(newDay)))
		}
	}
	return nil
}

// 更新证书
func updateCertificates() error {
	initGuide(false)
	// 获取所有证书
	certificates, err := db.Interface.GetAllCertificates()
	if err != nil {
		return fmt.Errorf("获取证书信息失败: %s", err)
	}

	updateNum := 0
	// 更新每个证书
	for _, cert := range certificates {
		fmt.Printf("正在更新域名 %s 的证书...\n", cert.Domain)

		BeforeExpirationDay, _ := config.GetConfig("", "before_expiration_day")
		day, err := strconv.ParseInt(BeforeExpirationDay, 10, 64)
		if err != nil {
			day = int64(defaultBeforeExpirationDay)
		}
		if cert.ExpireTime-(86400*day) > time.Now().Unix() {
			fmt.Printf("域名 %s 的证书未过期，跳过更新\n", cert.Domain)
			continue
		}

		var newCert db.Certificate
		newCert, err = getCertificateInfo(cert.Domain, cert.CertSource)
		if err != nil {
			fmt.Printf("获取域名 %s 的证书信息失败: %v\n", cert.Domain, err)
			continue
		}
		// 比较证书信息
		if newCert.PublicKey == cert.PublicKey && newCert.PrivateKey == cert.PrivateKey {
			fmt.Printf("域名 %s 的证书信息未更新，无需重新下载\n", cert.Domain)
			continue
		}

		// 设置证书路径和 ID
		newCert.CertPath = cert.CertPath
		newCert.KeyPath = cert.KeyPath
		newCert.ID = cert.ID

		// 更新证书信息
		err = db.Interface.UpdateCertificate(newCert)
		if err != nil {
			fmt.Printf("更新域名 %s 的证书信息失败: %v\n", cert.Domain, err)
			continue
		}

		// 更新证书文件
		err = updateCertificateFiles(newCert)
		if err != nil {
			return err
		}
		updateNum++
	}

	if updateNum == 0 {
		fmt.Println("本次没有需要更新的证书")
	} else {
		// 执行重载命令
		err = executeRestartCmd()
		if err != nil {
			return err
		}

		fmt.Println("更新证书完成")
	}

	return err
}

// 更新证书文件
func updateCertificateFiles(cert db.Certificate) error {
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

	fmt.Printf("域名 %s 的证书文件已更新\n", cert.Domain)
	return err
}

// 执行重载命令
func executeRestartCmd() error {
	// 获取重载命令
	restartCmd, err := config.GetConfig("", "restart_cmd")
	if err != nil {
		if err.Error() == "Key not found" {
			return fmt.Errorf("重载命令不存在，请先配置")
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

// 查找Nginx配置目录
func findNginxPathCmd() (err error) {

	reader := bufio.NewReader(os.Stdin)
	// 输入自定义Nginx配置文件路径
	fmt.Printf("请输入 Nginx 配置文件路径(如: /etc/nginx/nginx.conf, 多个路径用空格分隔，支持通配*.conf ): \n")
	nginxPath, _ := reader.ReadString('\n')
	nginxPath = strings.TrimSpace(nginxPath)
	var nginxPaths []string
	if nginxPath != "" {
		nginxPaths = strings.Split(nginxPath, " ")
	}
	if len(nginxPaths) > 0 {
		// 寻找 Nginx 配置文件
		findNginxConfigs(nginxPaths)
		color.Green("Nginx配置文件查找完成")
	} else {
		color.Yellow("目录为空，已跳过")
	}

	err = showCertificates()
	if err != nil {
		color.Red("%s", err)
		return
	}
	return err
}

// 任务计划
func cronTask() {
	defaultCronTime := "0 0 4 * *"
	defaultLogFile := "./cron.log"
	// 创建一个默认的cron对象
	c := cron.New()

	// 添加任务
	_, err := c.AddFunc(defaultCronTime, func() {
		logFile, err := os.OpenFile(defaultLogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			fmt.Println("open log file failed, err:", err)
			return
		}
		// 保存原始标准输出
		oldStdout := os.Stdout
		// 将标准输出重定向到文件
		os.Stdout = logFile
		color.Output = logFile
		log.SetOutput(logFile)
		log.SetFlags(log.Llongfile | log.Lmicroseconds | log.Ldate)
		log.Println("任务开始执行")
		log.SetPrefix("Cron: ")

		err = updateCertificates()
		if err != nil {
			log.Println(fmt.Sprintf("任务执行失败: %s", err))
			return
		}
		// 恢复标准输出
		os.Stdout = oldStdout
	})
	if err != nil {
		color.Red("添加任务调度失败: %s", err)
		return
	}
	color.Green("任务挂载成功，现在可以退出程序了，任务会在每天凌晨4点自动执行")
	//开始执行任务
	c.Start()

	//阻塞
	select {}
}

// 获取配置信息
func getConfigInfo() error {
	configs, err := config.GetConfigs()
	if err != nil {
		return fmt.Errorf("获取配置失败: %s", err)
	}
	for key, value := range configs {
		if strings.Contains(key, "key_secret") || strings.Contains(key, "api_key") {
			value = "********"
		}
		color.Cyan("%s: %s\n", key, value)
	}
	return err
}

// 修改密钥
func modifyKey() {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("请选择要配置的平台，目前支持certd、west，可以单一使用，也可混用，多个平台用空格分隔: ")
		thirdC, _ := reader.ReadString('\n')
		thirdC = strings.TrimSpace(thirdC)
		var thirdCs []string
		if thirdC != "" {
			thirdCs = strings.Split(thirdC, " ")
		}
		for _, t := range thirdCs {
			if t != "certd" && t != "west" {
				color.Red("平台错误，目前支持certd、west，多个平台用空格分隔")
				continue
			} else if t == "certd" {
				certd.SetConfig()
			} else if t == "west" {
				west.SetConfig()
			}
		}
		break
	}
}

// 检查是否初始化
func checkInit() bool {
	isInit, err := config.GetConfig("", "is_init")
	if err != nil {
		return false
	}
	if isInit == "1" {
		return true
	} else {
		return false
	}
}

// 检查证书是否已经存在（通过域名）
func checkHasDomain(domain string) bool {
	certInfo, _ := db.Interface.GetDomainCertificate(domain)
	return certInfo.Domain != ""
}

// 初始化引导
func initGuide(isEnd bool) {
	if !checkInit() {
		if !isEnd {
			color.Yellow("程序未初始化，现在开始初始化流程")
			initConfig()
		} else {
			color.Yellow("程序未初始化，请先初始化程序")
			os.Exit(0)
		}
	}
}
