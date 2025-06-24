package main

import (
	"database/sql"
	"fmt"
	_ "github.com/mattn/go-sqlite3"
	"os"
	"path/filepath"
)

var db *sql.DB

// 初始化数据库
func initDB() error {
	// 获取用户主目录
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("获取用户主目录失败: %v", err)
	}

	// 创建数据目录
	dataDir := filepath.Join(homeDir, ".ssl_assistant")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("创建数据目录失败: %v", err)
	}

	// 打开数据库
	dbPath := filepath.Join(dataDir, "ssl_assistant.db")
	db, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("打开数据库失败: %v", err)
	}

	// 创建表
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS certificates (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			domain TEXT NOT NULL,
			status TEXT NOT NULL,
			create_time INTEGER NOT NULL,
			expire_time INTEGER NOT NULL,
			public_key TEXT NOT NULL,
			private_key TEXT NOT NULL,
			cert_path TEXT NOT NULL,
			key_path TEXT NOT NULL,
			cert_source TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS config (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
	`)
	if err != nil {
		return fmt.Errorf("创建表失败: %v", err)
	}

	return nil
}

// 保存配置信息
func saveConfig(key, value string) error {
	_, err := db.Exec("INSERT OR REPLACE INTO config (key, value) VALUES (?, ?)", key, value)
	return err
}

// 获取配置信息
func getConfig(key string) (string, error) {
	var value string
	err := db.QueryRow("SELECT value FROM config WHERE key = ?", key).Scan(&value)
	return value, err
}

// 添加证书
func addCertificateToDB(cert Certificate) error {
	_, err := db.Exec(
		"INSERT INTO certificates (domain, status, create_time, expire_time, public_key, private_key, cert_path, key_path, cert_source) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		cert.Domain, cert.Status, cert.CreateTime, cert.ExpireTime, cert.PublicKey, cert.PrivateKey, cert.CertPath, cert.KeyPath, cert.CertSource,
	)
	return err
}

// 删除证书
func deleteCertificateFromDB(id int) error {
	_, err := db.Exec("DELETE FROM certificates WHERE id = ?", id)
	return err
}

// 获取所有证书
func getAllCertificates() ([]Certificate, error) {
	rows, err := db.Query("SELECT id, domain, status, create_time, expire_time, public_key, private_key, cert_path, key_path, cert_source FROM certificates")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var certificates []Certificate
	for rows.Next() {
		var cert Certificate
		var createTime, expireTime int64
		err := rows.Scan(&cert.ID, &cert.Domain, &cert.Status, &createTime, &expireTime, &cert.PublicKey, &cert.PrivateKey, &cert.CertPath, &cert.KeyPath, &cert.CertSource)
		if err != nil {
			return nil, err
		}

		// 解析时间
		cert.CreateTime = createTime
		cert.ExpireTime = expireTime

		certificates = append(certificates, cert)
	}

	return certificates, nil
}

// 获取证书
func getCertificate(id int) (Certificate, error) {
	var cert Certificate
	var createTime, expireTime int64
	err := db.QueryRow(
		"SELECT id, domain, status, create_time, expire_time, public_key, private_key, cert_path, key_path, cert_source FROM certificates WHERE id = ?",
		id,
	).Scan(&cert.ID, &cert.Domain, &cert.Status, &createTime, &expireTime, &cert.PublicKey, &cert.PrivateKey, &cert.CertPath, &cert.KeyPath, &cert.CertSource)
	if err != nil {
		return cert, err
	}
	cert.CreateTime = createTime
	cert.ExpireTime = expireTime

	return cert, nil
}

// 获取证书（通过域名）
func getDomainCertificate(domain string) (Certificate, error) {
	var cert Certificate
	var createTime, expireTime int64
	err := db.QueryRow(
		"SELECT id, domain, status, create_time, expire_time, public_key, private_key, cert_path, key_path, cert_source FROM certificates WHERE domain = ?",
		domain,
	).Scan(&cert.ID, &cert.Domain, &cert.Status, &createTime, &expireTime, &cert.PublicKey, &cert.PrivateKey, &cert.CertPath, &cert.KeyPath, &cert.CertSource)
	if err != nil {
		return cert, err
	}
	cert.CreateTime = createTime
	cert.ExpireTime = expireTime

	return cert, nil
}

// 更新证书
func updateCertificateInDB(cert Certificate) error {
	_, err := db.Exec(
		"UPDATE certificates SET domain = ?, status = ?, create_time = ?, expire_time = ?, public_key = ?, private_key = ?, cert_path = ?, key_path = ?, cert_source = ? WHERE id = ?",
		cert.Domain, cert.Status, cert.CreateTime, cert.ExpireTime, cert.PublicKey, cert.PrivateKey, cert.CertPath, cert.KeyPath, cert.CertSource, cert.ID,
	)
	return err
}
