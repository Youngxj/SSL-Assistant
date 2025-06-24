package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dgraph-io/badger/v3"
)

// 使用纯Go实现的键值存储作为SQLite的替代方案
// 当CGO_ENABLED=0时使用此实现

var badgerDB *badger.DB

// 初始化Badger数据库（纯Go实现，不需要CGO）
func initBadgerDB() error {
	// 获取用户主目录
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("获取用户主目录失败: %v", err)
	}

	// 创建数据目录
	dataDir := filepath.Join(homeDir, ".ssl_assistant", "badger")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("创建数据目录失败: %v", err)
	}

	// 打开Badger数据库
	opts := badger.DefaultOptions(dataDir)
	opts.Logger = nil // 禁用日志
	db, err := badger.Open(opts)
	if err != nil {
		return fmt.Errorf("打开Badger数据库失败: %v", err)
	}

	badgerDB = db
	return nil
}

// 关闭Badger数据库
func closeBadgerDB() {
	if badgerDB != nil {
		badgerDB.Close()
	}
}

// 保存配置信息到Badger
func saveConfigToBadger(key, value string) error {
	return badgerDB.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte("config:"+key), []byte(value))
	})
}

// 从Badger获取配置信息
func getConfigFromBadger(key string) (string, error) {
	var value string
	err := badgerDB.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("config:" + key))
		if err != nil {
			return err
		}

		return item.Value(func(val []byte) error {
			value = string(val)
			return nil
		})
	})

	return value, err
}

// 添加证书到Badger
func addCertificateToBadgerDB(cert Certificate) error {
	// 生成唯一ID
	timeNow := time.Now().UnixNano()
	cert.ID = int(timeNow % 1000000)

	// 序列化证书
	certData, err := json.Marshal(cert)
	if err != nil {
		return err
	}

	// 保存证书
	return badgerDB.Update(func(txn *badger.Txn) error {
		// 保存证书数据
		err := txn.Set([]byte(fmt.Sprintf("cert:%d", cert.ID)), certData)
		if err != nil {
			return err
		}

		// 保存域名索引
		return txn.Set([]byte(fmt.Sprintf("domain:%s", cert.Domain)), []byte(fmt.Sprintf("%d", cert.ID)))
	})
}

// 从Badger删除证书
func deleteCertificateFromBadgerDB(id int) error {
	// 先获取证书信息以获取域名
	cert, err := getCertificateFromBadger(id)
	if err != nil {
		return err
	}

	// 删除证书和域名索引
	return badgerDB.Update(func(txn *badger.Txn) error {
		// 删除证书数据
		err := txn.Delete([]byte(fmt.Sprintf("cert:%d", id)))
		if err != nil {
			return err
		}

		// 删除域名索引
		return txn.Delete([]byte(fmt.Sprintf("domain:%s", cert.Domain)))
	})
}

// 从Badger获取所有证书
func getAllCertificatesFromBadger() ([]Certificate, error) {
	var certificates []Certificate

	err := badgerDB.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 10
		it := txn.NewIterator(opts)
		defer it.Close()

		prefix := []byte("cert:")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()

			err := item.Value(func(val []byte) error {
				var cert Certificate
				if err := json.Unmarshal(val, &cert); err != nil {
					return err
				}
				certificates = append(certificates, cert)
				return nil
			})

			if err != nil {
				return err
			}
		}

		return nil
	})

	return certificates, err
}

// 从Badger获取证书
func getCertificateFromBadger(id int) (Certificate, error) {
	var cert Certificate

	err := badgerDB.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(fmt.Sprintf("cert:%d", id)))
		if err != nil {
			return err
		}

		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &cert)
		})
	})

	return cert, err
}

// 从Badger获取证书（通过域名）
func getDomainCertificateFromBadger(domain string) (Certificate, error) {
	var cert Certificate
	var certID int

	// 第一步：通过域名获取证书 ID
	err := badgerDB.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(fmt.Sprintf("domain:%s", domain)))
		if err != nil {
			return err
		}

		return item.Value(func(val []byte) error {
			// 将获取到的 ID 字符串转换为整数
			_, err := fmt.Sscanf(string(val), "%d", &certID)
			return err
		})
	})
	if err != nil {
		return cert, err
	}

	// 第二步：通过证书 ID 获取完整的证书信息
	err = badgerDB.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(fmt.Sprintf("cert:%d", certID)))
		if err != nil {
			return err
		}

		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &cert)
		})
	})

	return cert, err
}

// 更新Badger中的证书
func updateCertificateInBadgerDB(cert Certificate) error {
	// 序列化证书
	certData, err := json.Marshal(cert)
	if err != nil {
		return err
	}

	// 更新证书
	return badgerDB.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(fmt.Sprintf("cert:%d", cert.ID)), certData)
	})
}
