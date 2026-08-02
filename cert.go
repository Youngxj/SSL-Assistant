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
	"sync"
	"time"
)

// 常见的 Nginx 配置文件路径
var defaultNginxPaths = []string{
	"/www/server/panel/vhost/nginx/*.conf", // 宝塔 Nginx
	"/www/server/apache/vhost/*.conf",      // 宝塔 Apache
	"/opt/1panel/www/conf.d/*.conf",        // 1panel
	"/etc/nginx/nginx.conf",
	"/etc/nginx/conf.d/*.conf",
	"/usr/local/nginx/conf/nginx.conf",
	"/usr/local/etc/nginx/nginx.conf",
	"/etc/apache2/sites-enabled/*.conf", // Apache
	"/etc/apache2/sites-available/*.conf",
	"/etc/httpd/conf.d/*.conf",
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

// discoverPanelPaths 智能探测小皮面板（phpstudy）的 Nginx/Apache 站点配置目录。
// phpstudy 安装目录与 Nginx 版本号不确定（如 D:\phpstudy_pro\Extensions\Nginx1.15.11\conf\vhosts），
// 通过枚举盘符 + 遍历 Extensions 下 Nginx*/Apache* 目录动态定位；找不到时返回空，不影响原有扫描。
// 结果进程内缓存一次（phpstudy 安装/卸载后需重启程序重新识别）。
var (
	panelPathsOnce sync.Once
	panelPaths     []string
)

func discoverPanelPaths() []string {
	panelPathsOnce.Do(func() {
		if runtime.GOOS != "windows" {
			return
		}
		// 枚举存在的盘符（覆盖 C/D/E 及网络盘、U 盘等自定义安装位置）
		for c := 'A'; c <= 'Z'; c++ {
			drive := string(c) + ":\\"
			if _, err := os.Stat(drive); err != nil {
				continue
			}
			panelPaths = append(panelPaths, discoverPhpstudyFromRoot(filepath.Join(drive, "phpstudy_pro"))...)
		}
	})
	return panelPaths
}

// discoverPhpstudyFromRoot 给定 phpstudy 根目录，返回其 Nginx/Apache 的 vhosts 通配路径（版本号无关）
func discoverPhpstudyFromRoot(root string) []string {
	var paths []string
	extDir := filepath.Join(root, "Extensions")
	entries, err := os.ReadDir(extDir)
	if err != nil {
		return paths
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		// Nginx 或 Apache 开头（版本号可变），如 Nginx1.15.11、Apache2.4.39
		if !strings.HasPrefix(name, "Nginx") && !strings.HasPrefix(name, "Apache") {
			continue
		}
		vhosts := filepath.Join(extDir, name, "conf", "vhosts")
		if info, err := os.Stat(vhosts); err == nil && info.IsDir() {
			paths = append(paths, filepath.Join(vhosts, "*.conf"))
		}
	}
	return paths
}

// defaultCertDirs 证书文件目录（新版宝塔将证书统一存于 /www/server/panel/vhost/cert/<域名>/，Nginx/Apache 共用）
var defaultCertDirs = []string{"/www/server/panel/vhost/cert"}

// scanCertDir 扫描证书目录：第一层子目录名为域名，第二层为 fullchain.pem/privkey.pem 证书文件。
// 返回按目录名识别出的站点（域名 + 证书/私钥路径），目录不存在或文件缺失时返回空。
func scanCertDir(certRoot string) []nginxSite {
	var sites []nginxSite
	// 递归两层：certRoot/<域名>/fullchain.pem
	matches, err := filepath.Glob(filepath.Join(certRoot, "*", "fullchain.pem"))
	if err != nil {
		return sites
	}
	for _, certPath := range matches {
		domain := filepath.Base(filepath.Dir(certPath))
		keyPath := filepath.Join(filepath.Dir(certPath), "privkey.pem")
		if _, err := os.Stat(keyPath); err != nil {
			// 缺少私钥的证书目录跳过
			continue
		}
		sites = append(sites, nginxSite{
			Domain:   domain,
			Domains:  []string{domain},
			CertPath: certPath,
			KeyPath:  keyPath,
		})
	}
	return sites
}

// scanAllCertDirs 扫描全部证书目录（宝塔新版），聚合返回站点
func scanAllCertDirs() []nginxSite {
	var sites []nginxSite
	for _, dir := range defaultCertDirs {
		sites = append(sites, scanCertDir(dir)...)
	}
	return sites
}

// findNginxConfigs 寻找 Nginx 配置文件，聚合返回解析出的站点（按主域名去重合并，不立即添加）
// paths 为空时使用默认路径并自动探测面板（小皮 phpstudy）站点目录
func findNginxConfigs(paths []string) []nginxSite {
	color.Cyan("正在寻找 Nginx 配置文件...")

	// 智能探测面板（小皮 phpstudy）站点目录并合并
	paths = append(paths, discoverPanelPaths()...)

	// 证书目录扫描（新版宝塔统一证书目录，按域名目录自动识别）
	certSites := scanAllCertDirs()

	// 配置解析（宝塔/1Panel/原生 Nginx/Apache 等）
	var configSites []nginxSite
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
				found = append(found, parseConfigFile(match)...)
			}
		} else {
			// 否则直接检查路径是否存在；目录自动补默认通配符 *.conf
			if info, err := os.Stat(path); err == nil {
				if info.IsDir() {
					matches, _ := filepath.Glob(filepath.Join(path, "*.conf"))
					for _, match := range matches {
						found = append(found, parseConfigFile(match)...)
					}
				} else {
					found = append(found, parseConfigFile(path)...)
				}
			}
		}
		// 同一域名可能出现在多个配置文件，按主域名去重后收集
		for _, s := range found {
			if s.Domain == "" {
				continue
			}
			configSites = append(configSites, s)
		}
	}
	return mergeSites(certSites, configSites)
}

// mergeSites 合并证书目录站点与配置解析站点：
// 证书目录优先；配置解析站点仅在与证书目录站点 Domains 有交集时合并
// （目录名与 server_name 首项不一致场景，保留面板权威路径、Domains 取并集）。
// 配置解析站点之间维持按主域名去重，不互相交集合并（避免误吞不同证书的站点）。
func mergeSites(certSites, configSites []nginxSite) []nginxSite {
	var out []nginxSite

	// 1. 证书目录站点（面板权威路径，优先保留）；out 前段与 certSites 严格一一对应（第 3 步依赖该索引）
	for _, s := range certSites {
		out = append(out, s)
	}

	// 2. 配置解析站点：按主域名去重（同域名取 Domains 并集）
	var cfg []nginxSite
	cfgIndex := make(map[string]int)
	for _, s := range configSites {
		if s.Domain == "" {
			continue
		}
		if i, ok := cfgIndex[s.Domain]; ok {
			cfg[i].Domains = unionStrings(cfg[i].Domains, s.Domains)
			continue
		}
		cfgIndex[s.Domain] = len(cfg)
		cfg = append(cfg, s)
	}

	// 3. 仅 cfg 站点与证书目录站点有 Domains 交集时合并（out 前段与 certSites 索引一致）
	for _, s := range cfg {
		merged := false
		for i := range certSites {
			if shareDomains(certSites[i].Domains, s.Domains) {
				out[i].Domains = unionStrings(out[i].Domains, s.Domains)
				merged = true
				break
			}
		}
		if !merged {
			out = append(out, s)
		}
	}
	return out
}

// shareDomains 判断两个域名列表是否有交集
func shareDomains(a, b []string) bool {
	for _, x := range a {
		if containsString(b, x) {
			return true
		}
	}
	return false
}

// unionStrings 合并两个字符串列表（去重，保持顺序）
func unionStrings(a, b []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, s := range append(append([]string{}, a...), b...) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
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

// virtualHostBlockRegex 匹配 Apache VirtualHost 块（大小写不敏感，支持属性如 *:443）
var virtualHostBlockRegex = regexp.MustCompile(`(?is)<VirtualHost[^>]*>.*?</VirtualHost>`)

// Apache 指令正则（大小写不敏感，包级复用避免重复编译）
var (
	apacheServerNameRegex  = regexp.MustCompile(`(?i)ServerName\s+(\S+)`)
	apacheServerAliasRegex = regexp.MustCompile(`(?i)ServerAlias\s+([^\n<#]+)`)
	apacheSSLKeyRegex      = regexp.MustCompile(`(?i)SSLCertificateKeyFile\s+(\S+)`)
	apacheSSLCertRegex     = regexp.MustCompile(`(?i)SSLCertificateFile\s+(\S+)`)
)

// stripApacheComments 过滤 Apache 配置中的注释行（# 开头），避免注释中的指令被误匹配
func stripApacheComments(content string) string {
	var lines []string
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// findApacheBlocks 提取所有 <VirtualHost>...</VirtualHost> 块（Apache 不允许嵌套 VirtualHost）
func findApacheBlocks(content string) []string {
	return virtualHostBlockRegex.FindAllString(content, -1)
}

// isApacheConfig 判断配置内容是否为 Apache 语法（含 <VirtualHost 标签，大小写不敏感）
func isApacheConfig(content string) bool {
	return virtualHostOpenRegex.MatchString(content)
}

// virtualHostOpenRegex 匹配 <VirtualHost 开标签（大小写不敏感）
var virtualHostOpenRegex = regexp.MustCompile(`(?i)<VirtualHost\b`)

// parseConfigFile 按语法自动识别并解析 Nginx 或 Apache 配置文件
func parseConfigFile(path string) []nginxSite {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	if isApacheConfig(string(content)) {
		return parseApacheConfig(path)
	}
	return parseNginxConfig(path)
}

// 解析 Apache 配置文件，返回含证书配置的站点列表（仅收集，不添加）
func parseApacheConfig(path string) []nginxSite {
	fmt.Println("解析配置文件:", path)

	// 读取配置文件
	content, err := os.ReadFile(path)
	if err != nil {
		fmt.Println("读取配置文件失败:", err)
		return nil
	}

	// 过滤注释行（# 开头），避免注释中的指令被误匹配
	cleaned := stripApacheComments(string(content))

	// 按 VirtualHost 块为单位解析
	blocks := findApacheBlocks(cleaned)
	if len(blocks) == 0 {
		// 无 VirtualHost 块（如纯 Include），回退为整文件匹配
		blocks = []string{cleaned}
	}

	var sites []nginxSite
	for _, block := range blocks {
		serverNameMatch := apacheServerNameRegex.FindStringSubmatch(block)
		sslCertMatch := apacheSSLCertRegex.FindStringSubmatch(block)
		sslKeyMatch := apacheSSLKeyRegex.FindStringSubmatch(block)

		// 如果找到了 ServerName 和 SSLCertificateFile，则收集该站点
		if len(serverNameMatch) > 0 && len(sslCertMatch) > 0 && len(sslKeyMatch) > 0 {
			domains := []string{strings.TrimSpace(serverNameMatch[1])}
			// ServerAlias 可包含多个域名（[^\n<#]+ 已排除行内注释）
			if aliasMatch := apacheServerAliasRegex.FindStringSubmatch(block); len(aliasMatch) > 0 {
				for _, d := range strings.Fields(aliasMatch[1]) {
					domains = append(domains, d)
				}
			}
			sites = append(sites, nginxSite{
				Domain:   domains[0],
				Domains:  domains,
				CertPath: trimQuotes(strings.TrimSpace(sslCertMatch[1])),
				KeyPath:  trimQuotes(strings.TrimSpace(sslKeyMatch[1])),
			})
		}
	}
	return sites
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
			sslCert := trimQuotes(strings.TrimSpace(sslCertMatch[1]))
			sslKey := trimQuotes(strings.TrimSpace(sslKeyMatch[1]))

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

// buildCertFromLocalFiles 从本地证书/私钥文件解析证书信息（平台未配置或拉取失败时回退使用）。
// 返回的 CertSource 标记为 local，后续配置平台后 update 会自动探测 west/certd 正常更新。
func buildCertFromLocalFiles(domain, certPath, keyPath string) (db.Certificate, error) {
	var cert db.Certificate

	crt, err := os.ReadFile(certPath)
	if err != nil {
		return cert, fmt.Errorf("读取本地证书文件失败: %v", err)
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return cert, fmt.Errorf("读取本地私钥文件失败: %v", err)
	}
	endCert, err := utils.ParseCertificate(crt)
	if err != nil {
		return cert, fmt.Errorf("解析本地证书失败: %v", err)
	}

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
	cert.CertPath = certPath
	cert.KeyPath = keyPath
	cert.CertSource = "local"
	if len(endCert.DNSNames) > 0 {
		cert.CertDomains = strings.Join(endCert.DNSNames, ",")
	}
	return cert, nil
}

// addSiteFromNginx 将一个从 Nginx 配置解析出的站点添加为证书（查重 → 平台拉取或本地回退 → SAN 校验 → 保存）
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
		// 平台未配置或拉取失败：回退读取本地证书文件，保证已有证书也能被纳管
		color.Yellow("平台获取失败（%v），尝试从本地证书文件读取...\n", err)
		cert, err = buildCertFromLocalFiles(domain, site.CertPath, site.KeyPath)
		if err != nil {
			fmt.Printf("获取域名 %s 的证书信息失败: %v\n", domain, err)
			return
		}
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
	// 设置证书路径（平台来源时覆盖为 Nginx 配置中的路径）
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

	// 获取证书信息（优先平台拉取；平台未配置/失败时回退读取本地证书文件，保证已有证书也能添加）
	cert, err := getCertificateInfo(domain, "", 0)
	if err != nil {
		color.Yellow("平台获取失败（%v），尝试从本地证书文件读取...\n", err)
		certPath, keyPath, matched := findNginxCertPaths(domain)
		if matched {
			cert, err = buildCertFromLocalFiles(domain, certPath, keyPath)
		}
		if err != nil {
			return fmt.Errorf("获取证书信息失败（平台未配置或本地证书不存在）: %s", err)
		}
	}

	if checkHasDomain(domain) {
		return fmt.Errorf("域名 %s 的证书信息已存在，无需重复添加\n", domain)
	}

	// 平台来源且尚未设置路径：自动从宝塔/Nginx 配置匹配，未匹配到再手动输入
	if cert.CertPath == "" {
		certPath, keyPath, found := findNginxCertPaths(domain)
		if found {
			fmt.Printf("已自动从 Nginx 配置找到证书路径:\n  证书: %s\n  私钥: %s\n", certPath, keyPath)
			if utils.Confirm("是否使用自动匹配的路径") {
				cert.CertPath = certPath
				cert.KeyPath = keyPath
			}
		}
		if cert.CertPath == "" {
			cert.CertPath = utils.ReadInput("请输入证书存放路径（需包含文件名）: ", "")
			cert.KeyPath = utils.ReadInput("请输入私钥存放路径（需包含文件名）: ", "")
		}
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

// findNginxCertPaths 从默认配置路径（宝塔/1Panel/原生 Nginx、面板自动探测、宝塔证书目录）中查找指定域名的证书路径
func findNginxCertPaths(domain string) (certPath, keyPath string, found bool) {
	// 优先直查证书目录（新版宝塔 cert/<域名>/fullchain.pem，Nginx/Apache 共用）
	for _, certRoot := range defaultCertDirs {
		cp := filepath.Join(certRoot, domain, "fullchain.pem")
		kp := filepath.Join(certRoot, domain, "privkey.pem")
		if _, err := os.Stat(cp); err == nil {
			if _, err := os.Stat(kp); err == nil {
				return cp, kp, true
			}
		}
	}

	// 再查配置文件（宝塔/1Panel/原生 Nginx、面板自动探测）
	paths := append(defaultNginxPaths, discoverPanelPaths()...)
	for _, path := range paths {
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

// extractCertPathsFromFile 从配置文件中提取指定域名的 ssl 证书路径（自动识别 Nginx / Apache 语法）
func extractCertPathsFromFile(path, domain string) (string, string, bool) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", "", false
	}
	if isApacheConfig(string(content)) {
		return extractApacheCertPaths(content, domain)
	}
	return extractNginxCertPaths(content, domain)
}

// extractNginxCertPaths 从 Nginx 配置内容中提取指定域名的证书路径
func extractNginxCertPaths(content []byte, domain string) (string, string, bool) {
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
				return trimQuotes(strings.TrimSpace(certMatch[1])), trimQuotes(strings.TrimSpace(keyMatch[1])), true
			}
		}
	}
	return "", "", false
}

// extractApacheCertPaths 从 Apache 配置内容中提取指定域名的证书路径
func extractApacheCertPaths(content []byte, domain string) (string, string, bool) {
	// 过滤注释行，与 parseApacheConfig 保持一致
	cleaned := stripApacheComments(string(content))

	blocks := findApacheBlocks(cleaned)
	if len(blocks) == 0 {
		// 无 VirtualHost 块时回退整文件匹配（与 parseApacheConfig 行为对齐）
		blocks = []string{cleaned}
	}
	for _, block := range blocks {
		serverNameMatch := apacheServerNameRegex.FindStringSubmatch(block)
		if len(serverNameMatch) == 0 {
			continue
		}
		names := []string{strings.TrimSpace(serverNameMatch[1])}
		if aliasMatch := apacheServerAliasRegex.FindStringSubmatch(block); len(aliasMatch) > 0 {
			names = append(names, strings.Fields(aliasMatch[1])...)
		}
		for _, name := range names {
			if name != domain {
				continue
			}
			certMatch := apacheSSLCertRegex.FindStringSubmatch(block)
			keyMatch := apacheSSLKeyRegex.FindStringSubmatch(block)
			if len(certMatch) > 0 && len(keyMatch) > 0 {
				return trimQuotes(strings.TrimSpace(certMatch[1])), trimQuotes(strings.TrimSpace(keyMatch[1])), true
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

	// 空表时给出提示，避免渲染无意义的空表格
	if len(certs) == 0 {
		color.Yellow("暂无证书，可通过菜单「1=添加」或「7=快速添加域名」导入\n")
		return
	}

	// 显示证书信息表格（公钥/私钥列只显示文件名，避免超长路径撑爆表格；本地到期列为本地文件实际到期时间）
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"ID", "证书ID", "域名", "状态", "创建时间", "过期时间", "本地到期", "剩余天数", "来源", "证书文件", "私钥文件"})
	for _, cert := range certs {
		expireDay := time.Unix(cert.ExpireTime, 0).Sub(time.Now())
		var certStatus string
		if cert.ExpireTime < time.Now().Unix() {
			certStatus = "过期"
		} else {
			certStatus = "有效"
		}
		// 剩余天数：过期显示"已过期"，否则显示天数
		remainDays := strconv.FormatInt(int64(expireDay.Hours()/24), 10)
		if expireDay < 0 {
			remainDays = "已过期"
		}
		// 本地证书文件实际到期时间（每张证书一次轻量文件读取，性能开销可忽略）
		localExpire := "-"
		if cert.CertPath != "" {
			if e, err := getCertFileExpireTime(cert.CertPath); err == nil {
				localExpire = time.Unix(e, 0).Format(time.DateOnly)
			}
		}
		// 路径为空时保持空串显示，避免 filepath.Base("") 返回 "."
		certFile, keyFile := cert.CertPath, cert.KeyPath
		if cert.CertPath != "" {
			certFile = filepath.Base(cert.CertPath)
		}
		if cert.KeyPath != "" {
			keyFile = filepath.Base(cert.KeyPath)
		}

		table.Append([]string{
			strconv.Itoa(cert.ID),
			strconv.Itoa(cert.CertID),
			cert.Domain,
			certStatus,
			time.Unix(cert.CreateTime, 0).Format(time.DateOnly),
			time.Unix(cert.ExpireTime, 0).Format(time.DateOnly),
			localExpire,
			remainDays,
			cert.CertSource,
			certFile,
			keyFile,
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
	// 数据库模式与路径（show 菜单场景数据库已初始化）
	if mode := db.DBMode(); mode != "" {
		color.Cyan("数据库: %s (%s)\n", mode, db.DBPath())
	}
}

// 查看证书
func showCertificates() error {
	if err := initGuide(false); err != nil {
		return err
	}

	for {
		getCertificates()
		showPlatformStatus()
		// 菜单项按平台动态生成（终端方向键选择，非终端序号回退）：
		// Windows 下 cron 常驻/查任务不适用，隐藏"查看任务"
		items := []string{
			"添加证书",
			"删除证书",
			"修改密钥",
			"修改重载命令",
			"更新证书",
			"修改提前更新天数",
			"快速添加域名（Nginx目录检索）",
		}
		viewTaskIdx := -1
		if runtime.GOOS != "windows" {
			viewTaskIdx = len(items)
			items = append(items, "查看任务")
		}
		items = append(items, "查看配置信息", "退出")
		configIdx := len(items) - 2 // 查看配置信息
		exitIdx := len(items) - 1   // 退出

		idx := utils.SelectMenu(items, "请选择操作（↑/↓ 移动，回车确认）: ")

		// ESC 取消返回 -1，优先处理（避免与 viewTaskIdx=-1 冲突）
		if idx == -1 {
			color.Yellow("已取消\n")
			fmt.Println()
			continue
		}
		// 查看任务仅 Linux 显示（Windows 时 viewTaskIdx=-1 且上面已处理取消）
		if viewTaskIdx >= 0 && idx == viewTaskIdx {
			cPid := checkTask()
			if cPid == "" {
				color.Red("任务不存在，可以通过命令添加任务：./SSL-Assistant cron &")
			} else {
				color.Green("当前任务PID: %s", cPid)
			}
			fmt.Println()
			continue
		}

		switch idx {
		case 0: // 添加证书
			err := addCertificate()
			if err != nil {
				return err
			}
		case 1: // 删除证书
			err := deleteCertificate()
			if err != nil {
				return err
			}
		case 2: // 修改密钥
			modifyKey()
		case 3: // 修改重载命令
			err := modifyRestartCmd()
			if err != nil {
				return err
			}
		case 4: // 更新证书
			err := updateCertificates()
			if err != nil {
				return err
			}
		case 5: // 修改提前更新天数
			err := modifyExpirationDay()
			if err != nil {
				return err
			}
		case 6: // 快速添加域名
			err := findNginxPathCmd()
			if err != nil {
				return err
			}
		case configIdx: // 查看配置信息
			err := getConfigInfo()
			if err != nil {
				return err
			}
		case exitIdx: // 退出
			fmt.Println("程序退出")
			os.Exit(0)
		default:
			color.Yellow("已取消\n")
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
		// 比较基准：优先本地证书文件的实际内容（修复"DB 记录为云端证书、本地文件过期/非云端"时
		// 比较 DB 恒相同导致本地过期文件得不到更新的问题）；文件不可读时回退 DB 记录
		basePub, baseKey := readLocalCertFiles(cert.CertPath, cert.KeyPath)
		if basePub == "" && baseKey == "" {
			basePub, baseKey = cert.PublicKey, cert.PrivateKey
		}
		if newCert.PublicKey == basePub && newCert.PrivateKey == baseKey {
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

	// 绿色高亮提示更新成功的域名，便于在批量更新中快速识别
	color.Green("域名 %s 的证书文件已更新\n", cert.Domain)
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
			if entry.Value == "" {
				// 未配置时如实显示，避免空值被误认为已配置（打码）
				entry.Value = "未配置"
			} else {
				entry.Value = "********"
			}
		}
		color.Cyan("%s: %s\n", entry.Key, entry.Value)
	}
	return err
}

// 修改密钥
func modifyKey() {
	for {
		// 直接回车可跳过配置（不配置平台时，添加域名会回退读取本地证书文件）
		thirdC := utils.ReadInput("请选择要配置的平台，目前支持certd、west，可以单一使用，也可混用，多个平台用空格分隔（直接回车跳过配置）: ", "")
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

// trimQuotes 去掉字符串首尾的引号（Apache/Nginx 配置中的路径值可能带引号，如 SSLCertificateFile "path"）
func trimQuotes(s string) string {
	return strings.Trim(s, `"'`)
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

// readLocalCertFiles 读取本地证书/私钥文件的实际内容；文件缺失或读取失败返回空串
func readLocalCertFiles(certPath, keyPath string) (pub, key string) {
	if certPath != "" {
		if b, err := os.ReadFile(certPath); err == nil {
			pub = string(b)
		}
	}
	if keyPath != "" {
		if b, err := os.ReadFile(keyPath); err == nil {
			key = string(b)
		}
	}
	return
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
