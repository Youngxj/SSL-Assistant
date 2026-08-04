package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 类宝塔多 server 块配置（80 端口跳转块 + 443 SSL 块）
const multiServerConf = `
server {
    listen 80;
    server_name www.bt-test.com bt-test.com;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl;
    server_name www.bt-test.com;
    ssl_certificate /www/server/panel/vhost/cert/bt-test.com/fullchain.pem;
    ssl_certificate_key /www/server/panel/vhost/cert/bt-test.com/privkey.pem;
    location / { }
}
`

// 含 location/if 嵌套块：证书声明在嵌套块之后，非贪婪正则会在第一个 } 截断
const nestedBlockConf = `
server {
    listen 443 ssl;
    server_name www.nested.com;
    ssl_certificate /certs/nested/fullchain.pem;
    location /api {
        proxy_pass http://backend;
        if ($request_method = OPTIONS) {
            return 204;
        }
    }
    ssl_certificate_key /certs/nested/privkey.pem;
}
`

func TestFindServerBlocks(t *testing.T) {
	// 多 server 块应全部提取
	blocks := findServerBlocks(multiServerConf)
	if len(blocks) != 2 {
		t.Fatalf("应提取 2 个 server 块，实际 %d", len(blocks))
	}

	// 嵌套块：必须完整包含嵌套 location/if 之后的 ssl_certificate_key
	blocks = findServerBlocks(nestedBlockConf)
	if len(blocks) != 1 {
		t.Fatalf("应提取 1 个 server 块，实际 %d", len(blocks))
	}
	if !strings.Contains(blocks[0], "ssl_certificate_key /certs/nested/privkey.pem") {
		t.Fatalf("嵌套块被截断，未包含块尾的 ssl_certificate_key:\n%s", blocks[0])
	}

	// 无 server 块
	if blocks := findServerBlocks("http { include conf.d/*.conf; }"); len(blocks) != 0 {
		t.Fatalf("无 server 块时不应有结果，实际 %d", len(blocks))
	}

	// server_name 不应被误判为 server 块起始
	blocks = findServerBlocks("server_name example.com;")
	if len(blocks) != 0 {
		t.Fatalf("server_name 不应被误判为 server 块，实际 %d", len(blocks))
	}
}

func TestExtractCertPathsFromFile(t *testing.T) {
	dir := t.TempDir()

	// 多 server 块：正确命中 443 块的证书路径
	multiPath := filepath.Join(dir, "multi.conf")
	if err := os.WriteFile(multiPath, []byte(multiServerConf), 0644); err != nil {
		t.Fatal(err)
	}
	cp, kp, ok := extractCertPathsFromFile(multiPath, "www.bt-test.com")
	if !ok || cp != "/www/server/panel/vhost/cert/bt-test.com/fullchain.pem" || kp != "/www/server/panel/vhost/cert/bt-test.com/privkey.pem" {
		t.Fatalf("多 server 块提取失败: ok=%v cp=%s kp=%s", ok, cp, kp)
	}

	// 嵌套块
	nestedPath := filepath.Join(dir, "nested.conf")
	if err := os.WriteFile(nestedPath, []byte(nestedBlockConf), 0644); err != nil {
		t.Fatal(err)
	}
	cp, kp, ok = extractCertPathsFromFile(nestedPath, "www.nested.com")
	if !ok || cp != "/certs/nested/fullchain.pem" || kp != "/certs/nested/privkey.pem" {
		t.Fatalf("嵌套块提取失败: ok=%v cp=%s kp=%s", ok, cp, kp)
	}

	// 不存在的域名
	if _, _, ok := extractCertPathsFromFile(multiPath, "nope.com"); ok {
		t.Fatal("不应匹配不存在的域名")
	}
}

func TestGetCertFileExpireTime(t *testing.T) {
	dir := t.TempDir()

	// 生成 30 天后过期的自签证书
	certPath, _ := genSelfSignedCert(t, dir, "test.com", 30)
	notAfter := time.Now().Add(30 * 24 * time.Hour)

	expire, err := getCertFileExpireTime(certPath)
	if err != nil {
		t.Fatal(err)
	}
	if diff := expire - notAfter.Unix(); diff < -5 || diff > 5 {
		t.Fatalf("过期时间偏差过大: got=%d want=%d", expire, notAfter.Unix())
	}

	// 文件不存在应返回错误
	if _, err := getCertFileExpireTime(filepath.Join(dir, "missing.pem")); err == nil {
		t.Fatal("文件不存在应返回错误")
	}
}

func TestContainsString(t *testing.T) {
	list := []string{"a.com", "b.com"}
	if !containsString(list, "a.com") {
		t.Fatal("应包含 a.com")
	}
	if containsString(list, "c.com") {
		t.Fatal("不应包含 c.com")
	}
}
