package db

import (
	"fmt"
	"github.com/fatih/color"
)

// dbInterface 数据库接口定义
type dbInterface interface {
	SaveConfig(key, value string) error
	GetConfig(key string) (string, error)
	GetConfigs(keys []string) (map[string]string, error)
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
	ID         int    // 证书 ID
	Domain     string // 域名
	Status     string // 状态
	CreateTime int64  // 创建时间
	ExpireTime int64  // 过期时间
	PublicKey  string // 公钥
	PrivateKey string // 私钥
	CertPath   string // 证书路径
	KeyPath    string // 私钥路径
	CertSource string // 证书来源：certd
}

// SQLiteDB SQLite实现
type SQLiteDB struct{}

func (db *SQLiteDB) SaveConfig(key, value string) error {
	return saveConfig(key, value)
}

func (db *SQLiteDB) GetConfig(key string) (string, error) {
	return getConfig(key)
}

// GetConfigs 实现 GetConfigs 方法
func (db *SQLiteDB) GetConfigs(keys []string) (map[string]string, error) {
	configs := make(map[string]string)
	for _, key := range keys {
		value, err := db.GetConfig(key)
		if err != nil {
			return nil, err
		}
		configs[key] = value
	}
	return configs, nil
}

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
	// SQLite不需要显式关闭
}

// BadgerImpl BadgerDB实现
type BadgerImpl struct{}

func (db *BadgerImpl) SaveConfig(key, value string) error {
	return saveConfigToBadger(key, value)
}

func (db *BadgerImpl) GetConfig(key string) (string, error) {
	return getConfigFromBadger(key)
}

// GetConfigs 实现 GetConfigs 方法
func (db *BadgerImpl) GetConfigs(keys []string) (map[string]string, error) {
	configs := make(map[string]string)
	for _, key := range keys {
		value, err := db.GetConfig(key)
		if err != nil {
			return nil, err
		}
		configs[key] = value
	}
	return configs, nil
}

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

// InitDatabase 初始化数据库
func InitDatabase() error {
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
