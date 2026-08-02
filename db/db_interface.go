package db

import (
	"errors"
	"fmt"
	"github.com/fatih/color"
	"os"
	"path/filepath"
	"sync"
)

var (
	databaseOnce    sync.Once
	databaseInitErr error
)

// ErrNotFound 记录不存在的统一错误（SQLite 与 Badger 通用）
var ErrNotFound = errors.New("not found")

// dbInterface 数据库接口定义
type dbInterface interface {
	AddCertificate(cert Certificate) error
	DeleteCertificate(id int) error
	GetAllCertificates() ([]Certificate, error)
	GetCertificate(id int) (Certificate, error)
	GetDomainCertificate(domain string) (Certificate, error)
	UpdateCertificate(cert Certificate) error
	Close()
}

// Certificate 证书信息结构体
type Certificate struct {
	ID          int    // 证书 ID
	Domain      string // 域名
	Status      string // 状态
	CreateTime  int64  // 创建时间
	ExpireTime  int64  // 过期时间
	PublicKey   string // 公钥
	PrivateKey  string // 私钥
	CertPath    string // 证书路径
	KeyPath     string // 私钥路径
	CertSource  string // 证书来源：certd
	CertID      int    // 证书在来源平台的ID（如 certd 证书仓库ID），更新时优先使用
	CertDomains string // 证书覆盖的域名列表（逗号分隔，来自平台 detail）
}

// SQLiteDB SQLite实现
type SQLiteDB struct{}

func (db *SQLiteDB) AddCertificate(cert Certificate) error {
	return addCertificateToDB(cert)
}

func (db *SQLiteDB) DeleteCertificate(id int) error {
	return deleteCertificateFromDB(id)
}

func (db *SQLiteDB) GetAllCertificates() ([]Certificate, error) {
	return getAllCertificates()
}

func (db *SQLiteDB) GetCertificate(id int) (Certificate, error) {
	return getCertificate(id)
}
func (db *SQLiteDB) GetDomainCertificate(domain string) (Certificate, error) {
	return getDomainCertificate(domain)
}

func (db *SQLiteDB) UpdateCertificate(cert Certificate) error {
	return updateCertificateInDB(cert)
}

func (db *SQLiteDB) Close() {
	// 关闭 SQLite 连接（释放文件句柄）
	closeDB()
}

// BadgerImpl BadgerDB实现
type BadgerImpl struct{}

func (db *BadgerImpl) AddCertificate(cert Certificate) error {
	return addCertificateToBadgerDB(cert)
}

func (db *BadgerImpl) DeleteCertificate(id int) error {
	return deleteCertificateFromBadgerDB(id)
}

func (db *BadgerImpl) GetAllCertificates() ([]Certificate, error) {
	return getAllCertificatesFromBadger()
}

func (db *BadgerImpl) GetCertificate(id int) (Certificate, error) {
	return getCertificateFromBadger(id)
}
func (db *BadgerImpl) GetDomainCertificate(domain string) (Certificate, error) {
	return getDomainCertificateFromBadger(domain)
}

func (db *BadgerImpl) UpdateCertificate(cert Certificate) error {
	return updateCertificateInBadgerDB(cert)
}

func (db *BadgerImpl) Close() {
	closeBadgerDB()
}

// Interface 全局数据库接口
var Interface dbInterface

// DBMode 返回当前数据库模式（SQLite / BadgerDB），未初始化时返回空
func DBMode() string {
	if Interface == nil {
		return ""
	}
	if _, ok := Interface.(*BadgerImpl); ok {
		return "BadgerDB"
	}
	return "SQLite"
}

// DBPath 返回当前数据库数据路径（文件或目录）
func DBPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dataDir := filepath.Join(homeDir, ".ssl_assistant")
	if DBMode() == "BadgerDB" {
		return filepath.Join(dataDir, "badger")
	}
	return filepath.Join(dataDir, "ssl_assistant.db")
}

// InitDatabase 初始化数据库（进程内单例，只初始化一次）
func InitDatabase() error {
	databaseOnce.Do(func() {
		databaseInitErr = initDatabase()
	})
	return databaseInitErr
}

// initDatabase 尝试 SQLite，失败时自动降级到 BadgerDB
func initDatabase() error {
	// 尝试初始化SQLite数据库
	err := initDB()
	if err != nil {
		// 如果SQLite初始化失败，尝试使用BadgerDB
		fmt.Println("SQLite数据库初始化失败:", err)
		color.Cyan("尝试使用纯Go实现的BadgerDB作为替代...\n")

		err = initBadgerDB()
		if err != nil {
			return fmt.Errorf("BadgerDB初始化失败: %v", err)
		}

		// 使用BadgerDB实现
		Interface = &BadgerImpl{}
		color.Green("成功切换到BadgerDB\n")
	} else {
		// 使用SQLite实现
		Interface = &SQLiteDB{}
	}

	return nil
}

// OpenDatabase 初始化数据库（返回错误而非直接退出）
func OpenDatabase() error {
	return InitDatabase()
}
