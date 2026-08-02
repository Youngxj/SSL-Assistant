package main

import (
	"os"
	"path/filepath"
	"testing"
)

// 模拟新版宝塔证书目录：cert/<域名>/fullchain.pem + privkey.pem（Nginx/Apache 共用）
func TestScanCertDir(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "yum6.cn"), 0755)
	os.MkdirAll(filepath.Join(root, "www.example.com"), 0755)
	os.MkdirAll(filepath.Join(root, "nokey.com"), 0755) // 缺私钥，应跳过
	os.WriteFile(filepath.Join(root, "yum6.cn", "fullchain.pem"), []byte("crt"), 0644)
	os.WriteFile(filepath.Join(root, "yum6.cn", "privkey.pem"), []byte("key"), 0600)
	os.WriteFile(filepath.Join(root, "www.example.com", "fullchain.pem"), []byte("crt2"), 0644)
	os.WriteFile(filepath.Join(root, "www.example.com", "privkey.pem"), []byte("key2"), 0600)
	os.WriteFile(filepath.Join(root, "nokey.com", "fullchain.pem"), []byte("crt3"), 0644)

	sites := scanCertDir(root)
	if len(sites) != 2 {
		t.Fatalf("应识别 2 个站点（缺私钥的跳过），实际 %d: %+v", len(sites), sites)
	}
	for _, s := range sites {
		if s.Domain == "" || s.CertPath == "" || s.KeyPath == "" {
			t.Fatalf("站点字段不完整: %+v", s)
		}
	}
}

// 证书目录不存在时返回空
func TestScanCertDirMissing(t *testing.T) {
	if s := scanCertDir(filepath.Join(t.TempDir(), "nope")); len(s) != 0 {
		t.Fatalf("不存在的证书目录应返回空，实际 %v", s)
	}
}

// 证书目录（目录名 example.com）与配置解析（server_name 首项 www.example.com）应合并为一个站点，Domains 取并集
func TestMergeSites(t *testing.T) {
	certSites := []nginxSite{{Domain: "example.com", Domains: []string{"example.com"}, CertPath: "/cert/example.com/fullchain.pem", KeyPath: "/cert/example.com/privkey.pem"}}
	configSites := []nginxSite{{Domain: "www.example.com", Domains: []string{"www.example.com", "example.com", "api.example.com"}, CertPath: "/vhost/example.com/fullchain.pem", KeyPath: "/vhost/example.com/privkey.pem"}}

	out := mergeSites(certSites, configSites)
	if len(out) != 1 {
		t.Fatalf("应合并为 1 个站点（目录名与 server_name 首项不一致场景），实际 %d: %+v", len(out), out)
	}
	// 保留证书目录的路径（面板权威），Domains 并集含全部域名（SAN 校验用全量）
	if out[0].CertPath != "/cert/example.com/fullchain.pem" {
		t.Fatalf("应保留证书目录路径: %s", out[0].CertPath)
	}
	if !containsString(out[0].Domains, "api.example.com") || !containsString(out[0].Domains, "www.example.com") {
		t.Fatalf("Domains 应为并集: %v", out[0].Domains)
	}
}

// 不同域名各自保留；同主域名的配置解析站点 Domains 合并
func TestMergeSitesDistinct(t *testing.T) {
	certSites := []nginxSite{{Domain: "a.com", Domains: []string{"a.com"}}}
	configSites := []nginxSite{{Domain: "b.com", Domains: []string{"b.com"}}, {Domain: "b.com", Domains: []string{"b.com", "www.b.com"}}}

	out := mergeSites(certSites, configSites)
	if len(out) != 2 {
		t.Fatalf("应保留 2 个站点（b.com 配置解析去重合并），实际 %d: %+v", len(out), out)
	}
	if !containsString(out[1].Domains, "www.b.com") {
		t.Fatalf("b.com 的 Domains 应合并: %v", out[1].Domains)
	}
}

// 证书目录为空时（最常见场景），不同证书的 server 块即使 server_name 有交集也不应互相合并
func TestMergeSitesNoCertDirNoOverMerge(t *testing.T) {
	out := mergeSites(nil, []nginxSite{
		{Domain: "a.com", Domains: []string{"a.com", "b.com"}, CertPath: "/A/fullchain.pem"},
		{Domain: "b.com", Domains: []string{"b.com", "c.com"}, CertPath: "/B/fullchain.pem"},
	})
	if len(out) != 2 {
		t.Fatalf("证书目录为空时不同证书站点应各自保留（不互相交集合并），实际 %d: %+v", len(out), out)
	}
	paths := map[string]bool{}
	for _, s := range out {
		paths[s.CertPath] = true
	}
	if !paths["/A/fullchain.pem"] || !paths["/B/fullchain.pem"] {
		t.Fatalf("两个证书路径都应保留: %v", paths)
	}
}
