package main

import (
	"os"
	"path/filepath"
	"testing"
)

// 类 Apache 配置（含注释、多个 VirtualHost、大小写混用、ServerAlias）
const apacheConf = `
# 全局配置
Listen 443

<VirtualHost *:443>
    ServerName apache-a.com
    ServerAlias www.apache-a.com api.apache-a.com
    SSLEngine on
    SSLCertificateFile /etc/apache2/certs/apache-a.com/fullchain.pem
    SSLCertificateKeyFile /etc/apache2/certs/apache-a.com/privkey.pem
    # SSLCertificateFile /comment/should/not/match.pem
</VirtualHost>

<VirtualHost 1.2.3.4:443>
    servername apache-b.com
    sslcertificatefile /etc/httpd/certs/b.pem
    sslcertificatekeyfile /etc/httpd/certs/b.key
</VirtualHost>
`

func TestParseApacheConfig(t *testing.T) {
	dir := t.TempDir()
	confPath := filepath.Join(dir, "ssl.conf")
	if err := os.WriteFile(confPath, []byte(apacheConf), 0644); err != nil {
		t.Fatal(err)
	}

	sites := parseApacheConfig(confPath)
	if len(sites) != 2 {
		t.Fatalf("应解析出 2 个站点，实际 %d: %+v", len(sites), sites)
	}

	// 站点 A：ServerName + ServerAlias + 大小写不敏感指令
	a := sites[0]
	if a.Domain != "apache-a.com" {
		t.Fatalf("站点A主域名错误: %s", a.Domain)
	}
	if len(a.Domains) != 3 || a.Domains[2] != "api.apache-a.com" {
		t.Fatalf("站点A Domains（含 ServerAlias）错误: %v", a.Domains)
	}
	if a.CertPath != "/etc/apache2/certs/apache-a.com/fullchain.pem" {
		t.Fatalf("站点A证书路径错误: %s", a.CertPath)
	}
	// 注释中的路径不应被匹配
	if a.CertPath == "/comment/should/not/match.pem" {
		t.Fatal("注释中的证书路径被误匹配")
	}

	// 站点 B：小写指令
	b := sites[1]
	if b.Domain != "apache-b.com" || b.CertPath != "/etc/httpd/certs/b.pem" {
		t.Fatalf("站点B解析错误: %+v", b)
	}
}

func TestExtractApacheCertPaths(t *testing.T) {
	dir := t.TempDir()
	confPath := filepath.Join(dir, "ssl.conf")
	if err := os.WriteFile(confPath, []byte(apacheConf), 0644); err != nil {
		t.Fatal(err)
	}

	// 主域名匹配
	cp, kp, ok := extractCertPathsFromFile(confPath, "apache-a.com")
	if !ok || cp != "/etc/apache2/certs/apache-a.com/fullchain.pem" || kp != "/etc/apache2/certs/apache-a.com/privkey.pem" {
		t.Fatalf("Apache 路径提取失败: ok=%v cp=%s kp=%s", ok, cp, kp)
	}
	// ServerAlias 域名匹配
	if _, _, ok := extractCertPathsFromFile(confPath, "api.apache-a.com"); !ok {
		t.Fatal("ServerAlias 域名应匹配成功")
	}
	// 不存在的域名
	if _, _, ok := extractCertPathsFromFile(confPath, "nope.com"); ok {
		t.Fatal("不应匹配不存在的域名")
	}
}

func TestIsApacheConfig(t *testing.T) {
	if !isApacheConfig(apacheConf) {
		t.Fatal("含 <VirtualHost 应识别为 Apache 配置")
	}
	if isApacheConfig("server { server_name a.com; }") {
		t.Fatal("Nginx 配置不应识别为 Apache")
	}
	// 大小写不敏感
	if !isApacheConfig("<virtualhost *:443>...</virtualhost>") {
		t.Fatal("<virtualhost 小写也应识别为 Apache")
	}
}

// 注释中的 SSLCertificateFile 出现在真实路径之前，且关闭标签小写
const apacheCommentFirst = `
<VirtualHost *:443>
    # SSLCertificateFile /comment/first.pem
    ServerName comment-first.com
    SSLCertificateFile /etc/apache2/certs/comment-first.com/real.pem
    SSLCertificateKeyFile /etc/apache2/certs/comment-first.com/real.key
    ServerAlias alias.com # 行内注释
</virtualhost>
`

func TestExtractApacheCertPathsCommentFirst(t *testing.T) {
	dir := t.TempDir()
	confPath := filepath.Join(dir, "ssl.conf")
	if err := os.WriteFile(confPath, []byte(apacheCommentFirst), 0644); err != nil {
		t.Fatal(err)
	}
	// 注释在前的 SSLCertificateFile 不应被误取
	cp, _, ok := extractCertPathsFromFile(confPath, "comment-first.com")
	if !ok || cp != "/etc/apache2/certs/comment-first.com/real.pem" {
		t.Fatalf("应返回真实证书路径而非注释中的路径: ok=%v cp=%s", ok, cp)
	}
	// ServerAlias 行内注释不应进入域名（仅验证不报错，alias 命中真实域名即可）
	if _, _, ok := extractCertPathsFromFile(confPath, "alias.com"); !ok {
		t.Fatal("ServerAlias 域名应匹配成功")
	}
}

// 无 VirtualHost 块：应回退整文件匹配（与 parseApacheConfig 行为一致）
const apacheNoBlock = `
ServerName nohost.com
SSLCertificateFile /etc/httpd/nohost.pem
SSLCertificateKeyFile /etc/httpd/nohost.key
`

func TestExtractApacheCertPathsNoBlock(t *testing.T) {
	// 单元级验证 extractApacheCertPaths 内部回退（无 VirtualHost 块时整文件匹配）。
	// 注意：真实分发场景下，不含 <VirtualHost 的配置会被 isApacheConfig 判为 Nginx 语法，
	// 因此该回退属于解析器内部的防御性逻辑。
	cp, _, ok := extractApacheCertPaths([]byte(apacheNoBlock), "nohost.com")
	if !ok || cp != "/etc/httpd/nohost.pem" {
		t.Fatalf("无 VirtualHost 块时应回退整文件匹配: ok=%v cp=%s", ok, cp)
	}
}
