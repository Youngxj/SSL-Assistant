package db

import (
	"database/sql"
	"errors"
	"fmt"
	_ "github.com/mattn/go-sqlite3"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
			domain TEXT NOT NULL UNIQUE,
			status TEXT NOT NULL,
			create_time INTEGER NOT NULL,
			expire_time INTEGER NOT NULL,
			public_key TEXT NOT NULL,
			private_key TEXT NOT NULL,
			cert_path TEXT NOT NULL,
			key_path TEXT NOT NULL,
			cert_source TEXT NOT NULL,
			cert_id INTEGER NOT NULL DEFAULT 0,
			cert_domains TEXT NOT NULL DEFAULT ''
		);
	`)
	if err != nil {
		return fmt.Errorf("创建表失败: %v", err)
	}

	// 迁移旧表：补充新增的 cert_id / cert_domains 列（须在 UNIQUE 迁移之前，迁移读取数据依赖这些列）
	err = ensureCertColumns()
	if err != nil {
		return fmt.Errorf("迁移证书表列失败: %v", err)
	}

	// 迁移旧表：为 domain 添加 UNIQUE 约束（旧表无该约束）
	err = migrateCertificatesTable()
	if err != nil {
		return fmt.Errorf("迁移证书表失败: %v", err)
	}

	return nil
}

// ensureCertColumns 检查 certificates 表是否存在 cert_id / cert_domains 列，不存在则补充
func ensureCertColumns() error {
	rows, err := db.Query("PRAGMA table_info(certificates)")
	if err != nil {
		return err
	}
	cols := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		cols[name] = true
	}
	rows.Close()

	if !cols["cert_id"] {
		if _, err := db.Exec("ALTER TABLE certificates ADD COLUMN cert_id INTEGER NOT NULL DEFAULT 0"); err != nil {
			return err
		}
	}
	if !cols["cert_domains"] {
		if _, err := db.Exec("ALTER TABLE certificates ADD COLUMN cert_domains TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	return nil
}

// migrateCertificatesTable 检查旧版 certificates 表（无 UNIQUE 约束）并重建迁移
func migrateCertificatesTable() error {
	var sqlText string
	err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='certificates'`).Scan(&sqlText)
	if err != nil {
		// 表不存在则无需迁移
		return nil
	}
	if strings.Contains(sqlText, "UNIQUE") {
		return nil
	}

	// 读取现有数据
	rows, err := db.Query("SELECT id, domain, status, create_time, expire_time, public_key, private_key, cert_path, key_path, cert_source, cert_id, cert_domains FROM certificates")
	if err != nil {
		return err
	}
	var certs []Certificate
	for rows.Next() {
		var cert Certificate
		var createTime, expireTime int64
		if err := rows.Scan(&cert.ID, &cert.Domain, &cert.Status, &createTime, &expireTime, &cert.PublicKey, &cert.PrivateKey, &cert.CertPath, &cert.KeyPath, &cert.CertSource, &cert.CertID, &cert.CertDomains); err != nil {
			rows.Close()
			return err
		}
		cert.CreateTime = createTime
		cert.ExpireTime = expireTime
		certs = append(certs, cert)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// 按 id 倒序排序，保证 UNIQUE 冲突时保留最新记录
	sort.Slice(certs, func(i, j int) bool {
		return certs[i].ID > certs[j].ID
	})

	// 事务内重建表并回填数据，避免中途失败丢失数据
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DROP TABLE certificates"); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		CREATE TABLE certificates (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			domain TEXT NOT NULL UNIQUE,
			status TEXT NOT NULL,
			create_time INTEGER NOT NULL,
			expire_time INTEGER NOT NULL,
			public_key TEXT NOT NULL,
			private_key TEXT NOT NULL,
			cert_path TEXT NOT NULL,
			key_path TEXT NOT NULL,
			cert_source TEXT NOT NULL,
			cert_id INTEGER NOT NULL DEFAULT 0,
			cert_domains TEXT NOT NULL DEFAULT ''
		);
	`); err != nil {
		return err
	}

	// UNIQUE 冲突时忽略重复项，保留最新记录（已按 id 倒序）；显式写入原 id 保证用户记录编号不失效
	for _, cert := range certs {
		if _, err := tx.Exec(
			"INSERT OR IGNORE INTO certificates (id, domain, status, create_time, expire_time, public_key, private_key, cert_path, key_path, cert_source, cert_id, cert_domains) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			cert.ID, cert.Domain, cert.Status, cert.CreateTime, cert.ExpireTime, cert.PublicKey, cert.PrivateKey, cert.CertPath, cert.KeyPath, cert.CertSource, cert.CertID, cert.CertDomains,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// 添加证书
func addCertificateToDB(cert Certificate) error {
	_, err := db.Exec(
		"INSERT INTO certificates (domain, status, create_time, expire_time, public_key, private_key, cert_path, key_path, cert_source, cert_id, cert_domains) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		cert.Domain, cert.Status, cert.CreateTime, cert.ExpireTime, cert.PublicKey, cert.PrivateKey, cert.CertPath, cert.KeyPath, cert.CertSource, cert.CertID, cert.CertDomains,
	)
	return err
}

// 删除证书
func deleteCertificateFromDB(id int) error {
	result, err := db.Exec("DELETE FROM certificates WHERE id = ?", id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// 获取所有证书
func getAllCertificates() ([]Certificate, error) {
	rows, err := db.Query("SELECT id, domain, status, create_time, expire_time, public_key, private_key, cert_path, key_path, cert_source, cert_id, cert_domains FROM certificates")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var certificates []Certificate
	for rows.Next() {
		var cert Certificate
		var createTime, expireTime int64
		err := rows.Scan(&cert.ID, &cert.Domain, &cert.Status, &createTime, &expireTime, &cert.PublicKey, &cert.PrivateKey, &cert.CertPath, &cert.KeyPath, &cert.CertSource, &cert.CertID, &cert.CertDomains)
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
		"SELECT id, domain, status, create_time, expire_time, public_key, private_key, cert_path, key_path, cert_source, cert_id, cert_domains FROM certificates WHERE id = ?",
		id,
	).Scan(&cert.ID, &cert.Domain, &cert.Status, &createTime, &expireTime, &cert.PublicKey, &cert.PrivateKey, &cert.CertPath, &cert.KeyPath, &cert.CertSource, &cert.CertID, &cert.CertDomains)
	if errors.Is(err, sql.ErrNoRows) {
		return cert, ErrNotFound
	}
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
		"SELECT id, domain, status, create_time, expire_time, public_key, private_key, cert_path, key_path, cert_source, cert_id, cert_domains FROM certificates WHERE domain = ?",
		domain,
	).Scan(&cert.ID, &cert.Domain, &cert.Status, &createTime, &expireTime, &cert.PublicKey, &cert.PrivateKey, &cert.CertPath, &cert.KeyPath, &cert.CertSource, &cert.CertID, &cert.CertDomains)
	if errors.Is(err, sql.ErrNoRows) {
		return cert, ErrNotFound
	}
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
		"UPDATE certificates SET domain = ?, status = ?, create_time = ?, expire_time = ?, public_key = ?, private_key = ?, cert_path = ?, key_path = ?, cert_source = ?, cert_id = ?, cert_domains = ? WHERE id = ?",
		cert.Domain, cert.Status, cert.CreateTime, cert.ExpireTime, cert.PublicKey, cert.PrivateKey, cert.CertPath, cert.KeyPath, cert.CertSource, cert.CertID, cert.CertDomains, cert.ID,
	)
	return err
}
