package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

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
	opts.Logger = nil                // 禁用日志
	opts.ValueLogFileSize = 64 << 20 // 值日志文件默认1GB，证书数据量小，缩小到64MB避免浪费磁盘空间
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

// getNextBadgerID 生成唯一自增ID（事务内原子读写，避免时间戳碰撞覆盖）
func getNextBadgerID() (int, error) {
	var id int
	err := badgerDB.Update(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("meta:next_id"))
		if errors.Is(err, badger.ErrKeyNotFound) {
			id = 1
		} else if err != nil {
			return err
		} else {
			val, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}
			id, err = strconv.Atoi(string(val))
			if err != nil {
				return err
			}
			id++
		}
		return txn.Set([]byte("meta:next_id"), []byte(strconv.Itoa(id)))
	})
	return id, err
}

// 添加证书到Badger
func addCertificateToBadgerDB(cert Certificate) error {
	// 生成唯一ID
	id, err := getNextBadgerID()
	if err != nil {
		return err
	}
	cert.ID = id

	// 序列化证书
	certData, err := json.Marshal(cert)
	if err != nil {
		return err
	}

	// 保存证书
	return badgerDB.Update(func(txn *badger.Txn) error {
		// 检查域名是否已存在（与 SQLite 的 UNIQUE 约束行为保持一致）
		if _, err := txn.Get([]byte(fmt.Sprintf("domain:%s", cert.Domain))); err == nil {
			return fmt.Errorf("域名 %s 的证书信息已存在", cert.Domain)
		} else if !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}

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
	if errors.Is(err, badger.ErrKeyNotFound) {
		return cert, ErrNotFound
	}

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
	if errors.Is(err, badger.ErrKeyNotFound) {
		return cert, ErrNotFound
	}
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
