package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"ssl_assistant/db"
	"strings"
	"testing"
	"time"
)

// genSelfSignedCert 生成自签证书 + 私钥文件
func genSelfSignedCert(t *testing.T, dir, domain string) (certPath, keyPath string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: domain},
		DNSNames:     []string{domain, "www." + domain},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(90 * 24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPath = filepath.Join(dir, "fullchain.pem")
	keyPath = filepath.Join(dir, "privkey.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0644); err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalPKCS8PrivateKey(key)
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0600); err != nil {
		t.Fatal(err)
	}
	return
}

// TestBuildCertFromLocalFiles 验证从本地证书文件解析证书信息
func TestBuildCertFromLocalFiles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	dir := t.TempDir()
	certPath, keyPath := genSelfSignedCert(t, dir, "local-test.com")

	cert, err := buildCertFromLocalFiles("local-test.com", certPath, keyPath)
	if err != nil {
		t.Fatalf("解析本地证书失败: %v", err)
	}
	if cert.CertSource != "local" {
		t.Fatalf("CertSource 应为 local，实际: %s", cert.CertSource)
	}
	if !containsString(strings.Split(cert.CertDomains, ","), "www.local-test.com") {
		t.Fatalf("CertDomains 未从 SAN 提取: %s", cert.CertDomains)
	}
	if cert.PrivateKey == "" || cert.PublicKey == "" {
		t.Fatal("公钥/私钥未读取")
	}
	if cert.Status != "有效" {
		t.Fatalf("状态应为有效，实际: %s", cert.Status)
	}
}

// TestAddSiteFromNginxLocalFallback 平台未配置时，addSiteFromNginx 应回退本地证书文件成功添加
func TestAddSiteFromNginxLocalFallback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	// 测试结束关闭数据库（Badger 文件被进程持有会阻止 TempDir 清理）
	t.Cleanup(func() {
		if db.Interface != nil {
			db.Interface.Close()
		}
	})
	dir := t.TempDir()
	certPath, keyPath := genSelfSignedCert(t, dir, "local-test.com")

	site := nginxSite{
		Domain:   "local-test.com",
		Domains:  []string{"local-test.com", "www.local-test.com"},
		CertPath: certPath,
		KeyPath:  keyPath,
	}
	// 不配置任何平台 → getCertificateInfo 必然失败 → 应回退本地
	addSiteFromNginx(site)

	cert, err := db.GetCertificateWrapper("local-test.com")
	if err != nil {
		t.Fatalf("本地回退添加失败: %v", err)
	}
	if cert.CertSource != "local" {
		t.Fatalf("CertSource 应为 local，实际: %s", cert.CertSource)
	}
}

// readLocalCertFiles 读取本地证书/私钥文件内容，缺失返回空串
func TestReadLocalCertFiles(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "c.pem")
	keyPath := filepath.Join(dir, "k.key")
	os.WriteFile(certPath, []byte("PUBLIC"), 0644)
	os.WriteFile(keyPath, []byte("PRIVATE"), 0600)

	pub, key := readLocalCertFiles(certPath, keyPath)
	if pub != "PUBLIC" || key != "PRIVATE" {
		t.Fatalf("读取失败: pub=%q key=%q", pub, key)
	}
	pub, key = readLocalCertFiles(filepath.Join(dir, "nope.pem"), "")
	if pub != "" || key != "" {
		t.Fatalf("缺失文件应返回空: pub=%q key=%q", pub, key)
	}
}
