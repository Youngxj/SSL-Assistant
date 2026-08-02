package db

import (
	"errors"
	"os"
	"testing"
)

// TestMain 隔离用户主目录，避免测试污染真实 ~/.ssl_assistant 数据；
// 同一套用例在 CGO=1（SQLite）与 CGO=0（BadgerDB）下均可运行
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "ssl_db_test")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", tmp)
	os.Setenv("USERPROFILE", tmp)

	code := m.Run()

	os.RemoveAll(tmp)
	os.Exit(code)
}

func TestCertificateCRUD(t *testing.T) {
	// 初始化数据库（进程内单例，仅首次生效）
	if err := InitDatabase(); err != nil {
		t.Fatalf("初始化数据库失败: %v", err)
	}

	mkCert := func(domain string) Certificate {
		return Certificate{
			Domain:      domain,
			Status:      "有效",
			CreateTime:  100,
			ExpireTime:  200,
			PublicKey:   "pub-" + domain,
			PrivateKey:  "key-" + domain,
			CertPath:    "/tmp/" + domain + ".pem",
			KeyPath:     "/tmp/" + domain + ".key",
			CertSource:  "certd",
			CertID:      0,
			CertDomains: domain + ",www." + domain,
		}
	}

	// 添加
	c1 := mkCert("t1.com")
	if err := AddCertificateToDBWrapper(c1); err != nil {
		t.Fatalf("添加证书失败: %v", err)
	}

	// 相同域名重复添加应报错（SQLite UNIQUE / Badger 域名检查）
	if err := AddCertificateToDBWrapper(c1); err == nil {
		t.Fatal("重复添加同域名应报错")
	}

	// 添加第二张（含 CertID/CertDomains）
	c2 := mkCert("t2.com")
	c2.CertID = 88
	if err := AddCertificateToDBWrapper(c2); err != nil {
		t.Fatalf("添加第二张证书失败: %v", err)
	}

	// 按域名查询
	got, err := GetCertificateWrapper("t2.com")
	if err != nil {
		t.Fatalf("查询证书失败: %v", err)
	}
	if got.CertID != 88 || got.CertDomains != "t2.com,www.t2.com" {
		t.Fatalf("CertID/CertDomains 读写不一致: got=%+v", got)
	}

	// 查询不存在的域名 → ErrNotFound
	if _, err := GetCertificateWrapper("nope.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在域名应返回 ErrNotFound，实际: %v", err)
	}

	// 获取全部
	all, err := GetAllCertificatesWrapper()
	if err != nil {
		t.Fatalf("获取全部失败: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("应有 2 张证书，实际 %d", len(all))
	}

	// 更新
	got.Status = "过期"
	got.PublicKey = "pub-new"
	if err := UpdateCertificateInDBWrapper(got); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	got2, err := GetCertificateWrapper("t2.com")
	if err != nil {
		t.Fatalf("更新后查询失败: %v", err)
	}
	if got2.Status != "过期" || got2.PublicKey != "pub-new" {
		t.Fatalf("更新未生效: %+v", got2)
	}

	// 按 ID 查询
	byID, err := GetCertificateByIDWrapper(got.ID)
	if err != nil || byID.Domain != "t2.com" {
		t.Fatalf("按 ID 查询失败: err=%v byID=%+v", err, byID)
	}
	if _, err := GetCertificateByIDWrapper(99999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在 ID 应返回 ErrNotFound，实际: %v", err)
	}

	// 删除
	if err := DeleteCertificateFromDBWrapper(got.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	// 重复删除 → ErrNotFound
	if err := DeleteCertificateFromDBWrapper(got.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复删除应返回 ErrNotFound，实际: %v", err)
	}
}
