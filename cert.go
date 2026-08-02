package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
	"github.com/robfig/cron/v3"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
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
		if !utils.Confirm("程序已经初始化，是否重新初始化") {
			return
		}
	}

	// 设置平台密钥
	modifyKey()

	// 输入重载命令
	restartCmd := utils.ReadInput(fmt.Sprintf("请输入重载命令(如: %s): ", defaultReloadCmd), defaultReloadCmd)

	// 输入提前更新天数
	ExpirationDay := utils.ReadInput(fmt.Sprintf("请输入证书提前更新天数(默认: %d天): ", defaultBeforeExpirationDay), strconv.Itoa(int(defaultBeforeExpirationDay)))

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

	// 寻找 Nginx 配置文件，聚合返回站点列表（按主域名去重）
	sites := findNginxConfigs(defaultNginxPaths)

	// 默认路径已找到证书配置则不再询问自定义路径（issue #3：避免用户困惑于"必须输入路径"）
	if len(sites) > 0 {
		color.Green("已自动检索到 %d 个证书配置，无需再配置自定义路径\n", len(sites))
		// 列出所有检索到的域名，回车勾选后自动添加
		selectAndAddNginxSites(sites)
	} else {
		color.Yellow("默认路径未检索到证书配置，可自定义配置文件路径（直接回车跳过）")
		// 输入自定义Nginx配置文件路径
		err = findNginxPathCmd()
		if err != nil {
			color.Red("%s", err)
			return
		}
	}

	err = showCertificates()
	if err != nil {
		color.Red("%s", err)
		return
	}
}

// nginxSite 表示从 Nginx 配置解析出的一个站点（server 块）
type nginxSite struct {
	Domain   string   // 主域名（server_name 第一个）
	Domains  []string // server_name 全部域名（SAN 覆盖校验用）
	CertPath string   // ssl_certificate 路径
	KeyPath  string   // ssl_certificate_key 路径
}

// findNginxConfigs 寻找 Nginx 配置文件，聚合返回解析出的站点（按主域名去重，不立即添加）
func findNginxConfigs(paths []string) []nginxSite {
	color.Cyan("正在寻找 Nginx 配置文件...")
	var sites []nginxSite
	seen := make(map[string]bool)

	for _, path := range paths {
		fmt.Println("正在检索目录: ", path)
		var found []nginxSite
		// 如果路径包含通配符，则使用 Glob 函数
		if strings.Contains(path, "*") {
			matches, err := filepath.Glob(path)
			if err != nil {
				continue
			}

			for _, match := range matches {
				found = append(found, parseNginxConfig(match)...)
			}
		} else {
			// 否则直接检查路径是否存在；目录自动补默认通配符 *.conf
			if info, err := os.Stat(path); err == nil {
				if info.IsDir() {
					matches, _ := filepath.Glob(filepath.Join(path, "*.conf"))
					for _, match := range matches {
						found = append(found, parseNginxConfig(match)...)
					}
				} else {
					found = append(found, parseNginxConfig(path)...)
				}
			}
		}
		// 同一域名可能出现在多个配置文件，去重
		for _, s := range found {
			if s.Domain == "" || seen[s.Domain] {
				continue
			}
			seen[s.Domain] = true
			sites = append(sites, s)
		}
	}
	return sites
}

// serverBlockStartRegex 匹配 server 块起始（server 后跟空白与左花括号，不会误匹配 server_name）
var serverBlockStartRegex = regexp.MustCompile(`(?m)server\s*\{`)

// findServerBlocks 按大括号深度匹配提取所有 server { ... } 块（支持 location/if 等嵌套块）
func findServerBlocks(content string) []string {
	var blocks []string
	pos := 0
	for {
		loc := serverBlockStartRegex.FindStringIndex(content[pos:])
		if loc == nil {
			break
		}
		start := pos + loc[0]
		braceIdx := start + strings.Index(content[start:pos+loc[1]], "{")
		depth := 0
		end := -1
		for i := braceIdx; i < len(content); i++ {
			switch content[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					end = i
				}
			}
			if end > 0 {
				break
			}
		}
		if end < 0 {
			break
		}
		blocks = append(blocks, content[start:end+1])
		pos = end + 1
	}
	return blocks
}

// 解析 Nginx 配置文件，返回含证书配置的站点列表（仅收集，不添加；添加由用户勾选后执行）
func parseNginxConfig(path string) []nginxSite {
	fmt.Println("解析配置文件:", path)

	// 读取配置文件
	content, err := os.ReadFile(path)
	if err != nil {
		fmt.Println("读取配置文件失败:", err)
		return nil
	}

	// 按 server 块为单位解析，避免多个 server 块之间字段错位（支持嵌套块）
	serverNameRegex := regexp.MustCompile(`server_name\s+([^;]+);`)
	sslCertRegex := regexp.MustCompile(`ssl_certificate\s+([^;]+);`)
	sslKeyRegex := regexp.MustCompile(`ssl_certificate_key\s+([^;]+);`)

	blocks := findServerBlocks(string(content))
	if len(blocks) == 0 {
		// 无 server 块（如纯 include 或 http 块），回退为整文件匹配
		blocks = []string{string(content)}
	}

	var sites []nginxSite
	for _, block := range blocks {
		serverNameMatch := serverNameRegex.FindStringSubmatch(block)
		sslCertMatch := sslCertRegex.FindStringSubmatch(block)
		sslKeyMatch := sslKeyRegex.FindStringSubmatch(block)

		// 如果找到了 server_name 和 ssl_certificate，则收集该站点
		if len(serverNameMatch) > 0 && len(sslCertMatch) > 0 && len(sslKeyMatch) > 0 {
			serverName := strings.TrimSpace(serverNameMatch[1])
			sslCert := strings.TrimSpace(sslCertMatch[1])
			sslKey := strings.TrimSpace(sslKeyMatch[1])

			// 分割 server_name，可能有多个域名
			domains := strings.Fields(serverName)
			if len(domains) > 0 {
				sites = append(sites, nginxSite{
					Domain:   domains[0],
					Domains:  domains,
					CertPath: sslCert,
					KeyPath:  sslKey,
				})
			}
		}
	}
	return sites
}

// addSiteFromNginx 将一个从 Nginx 配置解析出的站点添加为证书（查重 → 拉取证书 → SAN 校验 → 保存）
func addSiteFromNginx(site nginxSite) {
	domain := site.Domain
	color.Cyan("添加域名: %s, 证书: %s, 私钥: %s\n", domain, site.CertPath, site.KeyPath)

	if checkHasDomain(domain) {
		color.Yellow("域名 %s 的证书信息已存在，无需重复添加\n", domain)
		return
	}

	// 获取证书信息（第三方平台 API 请求，可能需要几秒到几十秒）
	color.Cyan("正在从证书平台获取 %s 的证书信息...\n", domain)
	cert, err := getCertificateInfo(domain, "", 0)
	if err != nil {
		fmt.Printf("获取域名 %s 的证书信息失败: %v\n", domain, err)
		return
	}
	// SAN 校验：server_name 中的其他域名是否在证书覆盖范围内
	if cert.CertDomains != "" {
		covered := strings.Split(cert.CertDomains, ",")
		var missing []string
		for _, d := range site.Domains {
			if !containsString(covered, d) {
				missing = append(missing, d)
			}
		}
		if len(missing) > 0 {
			color.Yellow("警告: 域名 %s 不在证书覆盖范围内（证书仅覆盖: %s）\n", strings.Join(missing, ","), cert.CertDomains)
		}
	}
	// 设置证书路径
	cert.CertPath = site.CertPath
	cert.KeyPath = site.KeyPath

	// 保存证书信息
	err = db.AddCertificateToDBWrapper(cert)
	if err != nil {
		fmt.Printf("保存域名 %s 的证书信息失败: %v\n", domain, err)
		return
	}
	color.Green("域名 %s 的证书信息已保存\n", domain)
}

// selectAndAddNginxSites 展示检索到的站点域名，让用户回车勾选后批量添加。
// 非交互环境下自动全选（保持"自动添加"的原有行为，避免 EOF 退出）。
func selectAndAddNginxSites(sites []nginxSite) {
	if len(sites) == 0 {
		color.Yellow("未检索到证书配置\n")
		return
	}
	items := make([]string, len(sites))
	for i, s := range sites {
		items[i] = s.Domain
	}
	var selected []int
	if utils.IsInteractive() {
		color.Cyan("检索到 %d 个站点，请勾选要添加的域名：\n", len(sites))
		selected = utils.MultiSelectCheckbox(items, "")
	} else {
		// 非交互（如脚本/SSH 管道）：自动全选
		color.Cyan("检索到 %d 个站点，非交互环境下自动添加全部\n", len(sites))
		for i := range items {
			selected = append(selected, i)
		}
	}
	if len(selected) == 0 {
		color.Yellow("未选择任何域名，已跳过添加\n")
		return
	}
	color.Green("开始添加 %d 个域名...\n", len(selected))
	for i, idx := range selected {
		fmt.Printf("\n[%d/%d] ", i+1, len(selected))
		addSiteFromNginx(sites[idx])
	}
	color.Green("\n全部 %d 个域名添加完成\n", len(selected))
}

// 获取证书信息 certd/west
// @param domain 域名
// @param certSource 证书来源(west/certd)，传空自动判断
// @param certID 来源平台证书ID（certd证书仓库ID，更新时优先使用，0表示用域名查询）
// @return db.Certificate 证书信息
func getCertificateInfo(domain string, certSource string, certID int) (db.Certificate, error) {
	var cert db.Certificate
	var crt, key []byte
	var err error

	// 处理 Certd 返回的证书详情（ID/覆盖域名/有效期）
	var certDetailNotAfter int64
	applyCertdDetail := func(detail *certd.CertDetail) {
		if detail == nil {
			return
		}
		if detail.ID > 0 {
			cert.CertID = detail.ID
		}
		if len(detail.Domains) > 0 {
			cert.CertDomains = strings.Join(detail.Domains, ",")
		}
		if detail.NotAfter > 0 {
			certDetailNotAfter = detail.NotAfter
		}
	}

	switch certSource {
	case "west":
		color.Yellow("正在尝试使用West获取证书信息...\n")
		err, crt, _, key = west.GetCert(domain)
		if err != nil {
			return db.Certificate{}, err
		}
		cert.CertSource = "west"
	case "certd":
		color.Yellow("正在尝试使用Certd获取证书信息...\n")
		var detail *certd.CertDetail
		crt, key, detail, err = certd.GetCertificateInfo(domain, certID)
		if err != nil {
			if errors.Is(err, certd.ErrCertApplying) {
				color.Yellow("Certd已自动触发证书申请，请稍后重新执行获取\n")
			} else {
				color.Red("Certd:%s\n", err)
			}
			return db.Certificate{}, err
		}
		cert.CertSource = "certd"
		applyCertdDetail(detail)
	default:
		color.Yellow("正在尝试使用West获取证书信息...\n")
		err, crt, _, key = west.GetCert(domain)
		if err != nil {
			color.Red("West:%s\n", err)
			color.Yellow("正在尝试使用Certd获取证书信息...\n")
			var detail *certd.CertDetail
			crt, key, detail, err = certd.GetCertificateInfo(domain, certID)
			if err != nil {
				if errors.Is(err, certd.ErrCertApplying) {
					color.Yellow("Certd已自动触发证书申请，请稍后重新执行获取\n")
				} else {
					color.Red("Certd:%s\n", err)
				}
				return db.Certificate{}, err
			}
			cert.CertSource = "certd"
			applyCertdDetail(detail)
		} else {
			cert.CertSource = "west"
		}
	}
	if err != nil {
		return db.Certificate{}, err
	}
	endCert, err := utils.ParseCertificate(crt)
	if err != nil {
		return db.Certificate{}, fmt.Errorf("解析域名 %s 的证书失败: %v", domain, err)
	}
	utils.ShowCertificateInfo(endCert)
	// 设置证书信息
	cert.Domain = domain
	cert.CreateTime = endCert.NotBefore.UTC().Unix()
	cert.ExpireTime = endCert.NotAfter.UTC().Unix()
	if certDetailNotAfter > 0 {
		// certd detail.notAfter 为毫秒时间戳（源码 getTime()），需转为秒
		serverExpire := certDetailNotAfter / 1000
		// 合理性校验：与证书解析值偏差超过1天则采用本地解析值（避免平台数据异常导致判断错乱）
		if diff := serverExpire - cert.ExpireTime; diff < -86400 || diff > 86400 {
			color.Yellow("注意: certd返回的有效期(%d)与证书解析值(%d)偏差过大，采用本地解析值\n", serverExpire, cert.ExpireTime)
		} else {
			cert.ExpireTime = serverExpire
		}
	}
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
	if err := initGuide(false); err != nil {
		return err
	}
	// 输入域名
	domain := utils.ReadInput("请输入域名: ", "")

	// 获取证书信息
	cert, err := getCertificateInfo(domain, "", 0)
	if err != nil {
		return fmt.Errorf("获取证书信息失败: %s", err)
	}

	if checkHasDomain(domain) {
		return fmt.Errorf("域名 %s 的证书信息已存在，无需重复添加\n", domain)
	}

	// 自动从宝塔/Nginx 配置匹配该域名的证书路径（issue #3：无需手动输入路径）
	certPath, keyPath, found := findNginxCertPaths(domain)
	if found {
		fmt.Printf("已自动从 Nginx 配置找到证书路径:\n  证书: %s\n  私钥: %s\n", certPath, keyPath)
		if utils.Confirm("是否使用自动匹配的路径") {
			cert.CertPath = certPath
			cert.KeyPath = keyPath
		} else {
			found = false
		}
	}
	if !found {
		cert.CertPath = utils.ReadInput("请输入证书存放路径（需包含文件名）: ", "")
		cert.KeyPath = utils.ReadInput("请输入私钥存放路径（需包含文件名）: ", "")
	}

	// 保存证书信息
	err = db.AddCertificateToDBWrapper(cert)
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

// findNginxCertPaths 从默认 Nginx 配置路径（宝塔/1Panel/原生 Nginx）中查找指定域名的证书路径
func findNginxCertPaths(domain string) (certPath, keyPath string, found bool) {
	for _, path := range defaultNginxPaths {
		if strings.Contains(path, "*") {
			matches, err := filepath.Glob(path)
			if err != nil {
				continue
			}
			for _, match := range matches {
				if cp, kp, ok := extractCertPathsFromFile(match, domain); ok {
					return cp, kp, true
				}
			}
		} else {
			if _, err := os.Stat(path); err == nil {
				if cp, kp, ok := extractCertPathsFromFile(path, domain); ok {
					return cp, kp, true
				}
			}
		}
	}
	return "", "", false
}

// extractCertPathsFromFile 从单个 Nginx 配置文件中提取指定域名的 ssl 证书路径
func extractCertPathsFromFile(path, domain string) (string, string, bool) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", "", false
	}
	serverNameRegex := regexp.MustCompile(`server_name\s+([^;]+);`)
	sslCertRegex := regexp.MustCompile(`ssl_certificate\s+([^;]+);`)
	sslKeyRegex := regexp.MustCompile(`ssl_certificate_key\s+([^;]+);`)

	blocks := findServerBlocks(string(content))
	for _, block := range blocks {
		serverNameMatch := serverNameRegex.FindStringSubmatch(block)
		if len(serverNameMatch) == 0 {
			continue
		}
		for _, name := range strings.Fields(strings.TrimSpace(serverNameMatch[1])) {
			if name != domain {
				continue
			}
			certMatch := sslCertRegex.FindStringSubmatch(block)
			keyMatch := sslKeyRegex.FindStringSubmatch(block)
			if len(certMatch) > 0 && len(keyMatch) > 0 {
				return strings.TrimSpace(certMatch[1]), strings.TrimSpace(keyMatch[1]), true
			}
		}
	}
	return "", "", false
}

// 删除证书
func deleteCertificate() error {
	if err := initGuide(false); err != nil {
		return err
	}
	// 输入证书 ID
	idStr := utils.ReadInput("请输入证书 ID: ", "")

	// 转换为整数
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return fmt.Errorf("证书 ID 必须是整数")
	}

	// 获取证书信息（用于删除证书文件）
	cert, err := db.GetCertificateByIDWrapper(id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return fmt.Errorf("证书%s不存在", idStr)
		}
		return fmt.Errorf("获取证书信息失败: %s", err)
	}

	// 删除证书
	err = db.DeleteCertificateFromDBWrapper(id)
	if err != nil {
		return fmt.Errorf("删除证书失败: %s", err)
	}

	// 删除证书文件前检查是否被其他记录共享（多域名复用同一证书文件时只删记录、保留文件）
	if cert.CertPath != "" || cert.KeyPath != "" {
		shared, err := isCertFileShared(cert)
		if err != nil {
			color.Yellow("检查证书文件共享状态失败，仅删除数据库记录（文件已保留）: %v\n", err)
		} else if shared {
			color.Yellow("证书文件被其他站点共享，仅删除数据库记录，保留证书文件\n")
		} else {
			removeCertFile(cert.CertPath)
			removeCertFile(cert.KeyPath)
		}
	}

	color.Green("删除证书成功")
	return nil
}

// removeCertFile 删除证书文件，文件不存在时忽略，其他错误给出提示
func removeCertFile(path string) {
	if path == "" {
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		color.Yellow("删除证书文件失败 %s: %v\n", path, err)
	}
}

// isCertFileShared 检查证书文件路径是否被其他证书记录引用
func isCertFileShared(cert db.Certificate) (bool, error) {
	all, err := db.GetAllCertificatesWrapper()
	if err != nil {
		return false, err
	}
	certPath := filepath.Clean(cert.CertPath)
	keyPath := filepath.Clean(cert.KeyPath)
	for _, other := range all {
		if other.ID == cert.ID {
			continue
		}
		if certPath != "" && certPath != "." && filepath.Clean(other.CertPath) == certPath {
			return true, nil
		}
		if keyPath != "" && keyPath != "." && filepath.Clean(other.KeyPath) == keyPath {
			return true, nil
		}
	}
	return false, nil
}

// 获取证书并渲染表格
func getCertificates() {
	// 获取所有证书
	certs, err := db.GetAllCertificatesWrapper()
	if err != nil {
		fmt.Println("获取证书信息失败:", err)
		return
	}

	// 显示证书信息表格
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"ID", "证书ID", "域名", "状态", "创建时间", "过期时间", "剩余天数", "来源", "公钥", "私钥"})
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
			strconv.Itoa(cert.CertID),
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

// showPlatformStatus 输出当前证书平台配置状态（√ 已配置 / × 未配置完整）
func showPlatformStatus() {
	// 注意：color.Green/Red 顶层函数默认追加换行，这里用 GreenString/RedString 拼接到单行
	platformMark := func(ok bool, name string) string {
		if ok {
			return color.GreenString("√ " + name)
		}
		return color.RedString("× " + name)
	}

	// certd：api_url + key_id + key_secret 均配置才视为就绪
	apiURL, _ := config.GetConfig("third.certd", "api_url")
	keyID, _ := config.GetConfig("third.certd", "key_id")
	keySecret, _ := config.GetConfig("third.certd", "key_secret")
	certdOK := apiURL != "" && keyID != "" && keySecret != ""

	// west：username + api_key 均配置才视为就绪
	username, _ := config.GetConfig("third.west", "username")
	apiKey, _ := config.GetConfig("third.west", "api_key")
	westOK := username != "" && apiKey != ""

	fmt.Printf("平台配置: %s  %s\n", platformMark(certdOK, "certd"), platformMark(westOK, "west"))
}

// 查看证书
func showCertificates() error {
	if err := initGuide(false); err != nil {
		return err
	}

	for {
		getCertificates()
		showPlatformStatus()
		// 菜单项按平台动态生成：Windows 下 cron 常驻/查任务不适用，隐藏"查看任务"
		menu := "1=添加、2=删除、3=修改密钥、4=修改重载命令、5=更新证书、6=修改提前更新天数、7=快速添加域名（Nginx目录检索）"
		if runtime.GOOS != "windows" {
			menu += "、8=查看任务"
		}
		menu += "、9=查看配置信息、0=退出"
		fmt.Println("请输入操作：" + menu)
		input := utils.ReadInput(">>> ", "")
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
		case "8": // 查看任务（仅 Linux 显示；Windows 已隐藏，输入 8 时给出引导）
			if runtime.GOOS == "windows" {
				color.Yellow("Windows 环境不适用常驻任务，请使用任务计划程序定期执行 update（参见 README「计划任务设置」）\n")
				continue
			}
			cPid := checkTask()
			if cPid == "" {
				color.Red("任务不存在，可以通过命令添加任务：./SSL-Assistant cron &")
			} else {
				color.Green("当前任务PID: %s", cPid)
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
		fmt.Println()
	}
}

// 修改重载命令
func modifyRestartCmd() error {
	restartCmd, _ := config.GetConfig("", "restart_cmd")
	fmt.Printf("当前重载命令: %s\n", color.CyanString(restartCmd))
	newCmd := utils.ReadInput(fmt.Sprintf("请输入新的重载命令(如: %s): ", defaultReloadCmd), defaultReloadCmd)
	err := config.SetConfig("", "restart_cmd", newCmd)
	if err != nil {
		return fmt.Errorf("保存重载命令失败: %s", err)
	}
	color.Green("保存重载命令成功")
	return nil
}

// 修改过期前检查天数
func modifyExpirationDay() error {
	ExpirationDay, _ := config.GetConfig("", "before_expiration_day")
	fmt.Printf("当前过期前天数: %s\n", color.CyanString(ExpirationDay))
	newDay, err := strconv.Atoi(utils.ReadInput(fmt.Sprintf("请输入新的过期前天数(如: %d): ", defaultBeforeExpirationDay), strconv.Itoa(int(defaultBeforeExpirationDay))))
	if err != nil || newDay <= 0 {
		// 非法输入或 0 天时回退默认值，与旧逻辑保持一致
		newDay = int(defaultBeforeExpirationDay)
	}
	err = config.SetConfig("", "before_expiration_day", strconv.Itoa(newDay))
	if err != nil {
		return fmt.Errorf("保存过期前天数失败: %s", err)
	}
	color.Green("过期前天数已修改成: %s", color.CyanString(strconv.Itoa(newDay)))
	return nil
}

// 更新证书
func updateCertificates() error {
	if err := initGuide(false); err != nil {
		return err
	}
	// 获取所有证书
	certificates, err := db.GetAllCertificatesWrapper()
	if err != nil {
		return fmt.Errorf("获取证书信息失败: %s", err)
	}

	updateNum := 0
	failedNum := 0
	// 提前读取配置，避免循环内重复加载 ini 文件
	BeforeExpirationDay, _ := config.GetConfig("", "before_expiration_day")
	day, err := strconv.ParseInt(BeforeExpirationDay, 10, 64)
	if err != nil {
		day = int64(defaultBeforeExpirationDay)
	}
	// 更新每个证书
	for _, cert := range certificates {
		fmt.Printf("正在更新域名 %s 的证书...\n", cert.Domain)

		// 判断是否需要更新：优先以证书文件的实际过期时间为准，
		// 避免"网站文件已过期但数据库记录仍显示有效"导致漏更新（issue #3 评论）
		needUpdate := false
		if cert.CertPath != "" {
			if fileExpire, err := getCertFileExpireTime(cert.CertPath); err == nil {
				needUpdate = fileExpire-(86400*day) <= time.Now().Unix()
			} else {
				// 证书文件不存在或无法解析，回退用数据库记录的过期时间判断
				needUpdate = cert.ExpireTime-(86400*day) <= time.Now().Unix()
			}
		} else {
			needUpdate = cert.ExpireTime-(86400*day) <= time.Now().Unix()
		}
		if !needUpdate {
			fmt.Printf("域名 %s 的证书未过期，跳过更新\n", cert.Domain)
			continue
		}

		var newCert db.Certificate
		newCert, err = getCertificateInfo(cert.Domain, cert.CertSource, cert.CertID)
		if err != nil {
			fmt.Printf("获取域名 %s 的证书信息失败: %v\n", cert.Domain, err)
			failedNum++
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
		// 保留原有平台证书ID与覆盖域名（非certd来源或detail缺失时不会被清空）
		if newCert.CertID == 0 {
			newCert.CertID = cert.CertID
		}
		if newCert.CertDomains == "" {
			newCert.CertDomains = cert.CertDomains
		}

		// 更新证书信息
		err = db.UpdateCertificateInDBWrapper(newCert)
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

	if updateNum == 0 && failedNum == 0 {
		fmt.Println("本次没有需要更新的证书")
	} else {
		if updateNum > 0 {
			// 执行重载命令
			err = executeRestartCmd()
			if err != nil {
				return err
			}
		}
		if failedNum > 0 {
			return fmt.Errorf("更新完成，但有 %d 个证书获取/更新失败", failedNum)
		}
		fmt.Println("更新证书完成")
	}

	return nil
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

	// 更新私钥文件（私钥权限收紧为 0600，避免同机其他用户可读）
	err = os.WriteFile(cert.KeyPath, []byte(cert.PrivateKey), 0600)
	if err != nil {
		return fmt.Errorf("更新域名 %s 的私钥文件失败: %v\n", cert.Domain, err)
	}

	fmt.Printf("域名 %s 的证书文件已更新\n", cert.Domain)
	return err
}

// 执行重载命令
func executeRestartCmd() error {
	// 获取重载命令
	restartCmd, _ := config.GetConfig("", "restart_cmd")
	if strings.TrimSpace(restartCmd) == "" {
		return fmt.Errorf("重载命令不存在，请先配置")
	}

	// 限制执行超时（默认60秒），避免重载命令挂死
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 通过系统 shell 执行，支持引号、管道、$() 等语法（如 docker restart $(docker ps -aqf "name=openresty")）
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", restartCmd)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", restartCmd)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("执行重载命令失败: %v\n%s\n", err, output)
	}

	color.Green("执行重载命令成功: %s\n", output)
	return nil
}

// 查找Nginx配置目录
func findNginxPathCmd() (err error) {

	// 输入自定义Nginx配置文件路径，每行一个，避免 Windows 路径含空格被拆分；示例按平台显示
	nginxExample := "/etc/nginx/nginx.conf"
	nginxDirExample := "/etc/nginx/conf.d"
	if runtime.GOOS == "windows" {
		nginxExample = `C:\nginx\conf\nginx.conf`
		nginxDirExample = `C:\nginx\conf\vhosts`
	}
	fmt.Printf("请输入 Nginx 配置文件路径，每行一个。支持三种写法：\n  1. 目录（自动匹配该目录下 *.conf，如 %s）\n  2. 单个文件（如 %s）\n  3. 通配符（如 %s\\*.conf）\n输入完成后请直接回车(空行)结束:\n", nginxDirExample, nginxExample, nginxDirExample)
	var nginxPaths []string
	for {
		line := utils.ReadInput("", "")
		if line == "" {
			break
		}
		nginxPaths = append(nginxPaths, line)
		color.Cyan("已添加路径: %s（继续输入下一行，或直接回车结束输入）\n", line)
	}
	if len(nginxPaths) > 0 {
		// 寻找 Nginx 配置文件
		sites := findNginxConfigs(nginxPaths)
		color.Green("Nginx配置文件查找完成")
		// 列出检索到的域名，回车勾选后自动添加
		selectAndAddNginxSites(sites)
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

// 检查任务
func checkTask() string {
	cronPid, err := config.GetConfig("", "cron_pid")
	if cronPid == "" || err != nil {
		return ""
	}
	if runtime.GOOS == "linux" {
		pid, _ := strconv.Atoi(cronPid)
		if !utils.CheckPid(pid) {
			color.Red("证书更新任务进程不存在，可能已被手动kill，需要重新添加任务")
			return ""
		}
	}
	return cronPid
}

// 任务计划
func cronTask(force bool) {
	if !force {
		cPid := checkTask()
		if cPid != "" {
			color.Red("证书更新任务已存在，无需重复添加\n")
			color.Green("当前任务PID: %s", cPid)
			return
		}
	}
	// 创建一个默认的cron对象
	c := cron.New()
	defaultCronTime := "0 0 4 * *"
	defaultLogFile := "./cron.log"

	// 添加任务
	_, err := c.AddFunc(defaultCronTime, func() {
		logFile, err := os.OpenFile(defaultLogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			fmt.Println("open log file failed, err:", err)
			return
		}
		defer logFile.Close()

		// 保存原始标准输出，任务结束（含出错）后恢复，避免污染全局输出
		oldStdout := os.Stdout
		oldColorOutput := color.Output
		oldLogOutput := log.Writer()
		oldLogFlags := log.Flags()
		oldLogPrefix := log.Prefix()
		os.Stdout = logFile
		color.Output = logFile
		defer func() {
			os.Stdout = oldStdout
			color.Output = oldColorOutput
			log.SetOutput(oldLogOutput)
			log.SetFlags(oldLogFlags)
			log.SetPrefix(oldLogPrefix)
		}()

		log.SetOutput(logFile)
		log.SetFlags(log.Llongfile | log.Lmicroseconds | log.Ldate)
		log.Println("任务开始执行")
		log.SetPrefix("Cron: ")
		cronPid, _ := config.GetConfig("", "cron_pid")
		pid, _ := strconv.Atoi(cronPid)
		log.Println("cronPid", cronPid, "pid", pid, "os.Getpid()", os.Getpid())
		if cronPid != "" && pid != os.Getpid() {
			log.Printf("任务pid: %d 与当前进程pid: %d 不一致，跳过任务\n", pid, os.Getpid())
			return
		}

		err = updateCertificates()
		if err != nil {
			log.Printf("任务执行完成，但存在错误: %s", err)
			return
		}
	})
	if err != nil {
		color.Red("添加任务调度失败: %s", err)
		return
	}
	color.Green("任务挂载成功，现在可以退出程序了，证书检查会在每天凌晨4点自动执行\n")
	color.Green("当前进程 PID: %d", os.Getpid())
	err = config.SetConfig("", "cron_pid", strconv.Itoa(os.Getpid()))
	if err != nil {
		color.Red("记录任务调度配置失败: %s", err)
		return
	}
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
	for _, entry := range configs {
		if strings.Contains(entry.Key, "key_secret") || strings.Contains(entry.Key, "api_key") {
			entry.Value = "********"
		}
		color.Cyan("%s: %s\n", entry.Key, entry.Value)
	}
	return err
}

// 修改密钥
func modifyKey() {
	for {
		thirdC := utils.ReadInput("请选择要配置的平台，目前支持certd、west，可以单一使用，也可混用，多个平台用空格分隔: ", "")
		if thirdC == "" {
			return
		}
		thirdCs := strings.Split(thirdC, " ")
		valid := false
		for _, t := range thirdCs {
			if t != "certd" && t != "west" {
				color.Red("平台错误，目前支持certd、west，多个平台用空格分隔")
				continue
			}
			valid = true
			if t == "certd" {
				certd.SetConfig()
			} else if t == "west" {
				west.SetConfig()
			}
		}
		// 存在有效平台配置则退出，否则重新输入
		if valid {
			return
		}
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
	certInfo, err := db.GetCertificateWrapper(domain)
	if err != nil {
		if !errors.Is(err, db.ErrNotFound) {
			color.Yellow("检查域名 %s 是否存在时出错: %v\n", domain, err)
		}
		return false
	}
	return certInfo.Domain != ""
}

// containsString 判断字符串切片是否包含指定元素
func containsString(list []string, target string) bool {
	for _, s := range list {
		if s == target {
			return true
		}
	}
	return false
}

// getCertFileExpireTime 读取证书文件的过期时间（秒时间戳）
func getCertFileExpireTime(path string) (int64, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	endCert, err := utils.ParseCertificate(content)
	if err != nil {
		return 0, err
	}
	return endCert.NotAfter.UTC().Unix(), nil
}

// 初始化引导：未初始化时触发初始化或返回错误。
// 返回 error（而非 os.Exit）以便调用方决定退出还是返回交互菜单，避免双击菜单场景下整个程序被直接终止（闪退）。
func initGuide(isEnd bool) error {
	if !checkInit() {
		if !isEnd {
			if !utils.IsInteractive() {
				color.Red("程序未初始化，且当前环境不支持交互输入，请先手动执行 init 命令完成初始化")
				color.Yellow("提示: 如确认当前处于交互终端（如 git-bash），可设置环境变量 SSL_ASSISTANT_INTERACTIVE=1 强制进入交互模式\n")
				return fmt.Errorf("程序未初始化")
			}
			color.Yellow("程序未初始化，现在开始初始化流程")
			initConfig()
		} else {
			return fmt.Errorf("程序未初始化，请先执行 init 命令完成初始化")
		}
	}
	return nil
}
