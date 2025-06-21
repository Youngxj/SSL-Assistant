package main

// 数据库类型标志
var useBadgerDB bool

// 数据库函数类型定义
type SaveConfigFunc func(key, value string) error
type GetConfigFunc func(key string) (string, error)
type AddCertificateToDBFunc func(cert Certificate) error
type DeleteCertificateFromDBFunc func(id int) error
type GetAllCertificatesFunc func() ([]Certificate, error)
type GetCertificateFunc func(id int) (Certificate, error)
type UpdateCertificateInDBFunc func(cert Certificate) error

// 函数变量
var (
	// 保存原始SQLite函数
	sqliteSaveConfig              = saveConfig
	sqliteGetConfig               = getConfig
	sqliteAddCertificateToDB      = addCertificateToDB
	sqliteDeleteCertificateFromDB = deleteCertificateFromDB
	sqliteGetAllCertificates      = getAllCertificates
	sqliteGetCertificate          = getCertificate
	sqliteUpdateCertificateInDB   = updateCertificateInDB

	// 当前使用的函数
	currentSaveConfig              SaveConfigFunc
	currentGetConfig               GetConfigFunc
	currentAddCertificateToDB      AddCertificateToDBFunc
	currentDeleteCertificateFromDB DeleteCertificateFromDBFunc
	currentGetAllCertificates      GetAllCertificatesFunc
	currentGetCertificate          GetCertificateFunc
	currentUpdateCertificateInDB   UpdateCertificateInDBFunc
)

// 初始化数据库包装器
func initDBWrapper() {
	// 默认使用SQLite函数
	currentSaveConfig = sqliteSaveConfig
	currentGetConfig = sqliteGetConfig
	currentAddCertificateToDB = sqliteAddCertificateToDB
	currentDeleteCertificateFromDB = sqliteDeleteCertificateFromDB
	currentGetAllCertificates = sqliteGetAllCertificates
	currentGetCertificate = sqliteGetCertificate
	currentUpdateCertificateInDB = sqliteUpdateCertificateInDB
}

// 切换到BadgerDB
func switchToBadgerDB() {
	useBadgerDB = true
	currentSaveConfig = saveConfigToBadger
	currentGetConfig = getConfigFromBadger
	currentAddCertificateToDB = addCertificateToBadgerDB
	currentDeleteCertificateFromDB = deleteCertificateFromBadgerDB
	currentGetAllCertificates = getAllCertificatesFromBadger
	currentGetCertificate = getCertificateFromBadger
	currentUpdateCertificateInDB = updateCertificateInBadgerDB
}

// 包装函数 - 这些函数将被外部调用
func saveConfigWrapper(key, value string) error {
	return currentSaveConfig(key, value)
}

func getConfigWrapper(key string) (string, error) {
	return currentGetConfig(key)
}

func addCertificateToDBWrapper(cert Certificate) error {
	return currentAddCertificateToDB(cert)
}

func deleteCertificateFromDBWrapper(id int) error {
	return currentDeleteCertificateFromDB(id)
}

func getAllCertificatesWrapper() ([]Certificate, error) {
	return currentGetAllCertificates()
}

func getCertificateWrapper(id int) (Certificate, error) {
	return currentGetCertificate(id)
}

func updateCertificateInDBWrapper(cert Certificate) error {
	return currentUpdateCertificateInDB(cert)
}
