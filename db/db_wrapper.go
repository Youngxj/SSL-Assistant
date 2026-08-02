package db

// AddCertificateToDBWrapper 添加证书
func AddCertificateToDBWrapper(cert Certificate) error {
	if err := OpenDatabase(); err != nil {
		return err
	}
	return Interface.AddCertificate(cert)
}

// DeleteCertificateFromDBWrapper 删除证书
func DeleteCertificateFromDBWrapper(id int) error {
	if err := OpenDatabase(); err != nil {
		return err
	}
	return Interface.DeleteCertificate(id)
}

// GetAllCertificatesWrapper 获取所有证书
func GetAllCertificatesWrapper() ([]Certificate, error) {
	if err := OpenDatabase(); err != nil {
		return nil, err
	}
	return Interface.GetAllCertificates()
}

// GetCertificateByIDWrapper 通过ID获取指定证书
func GetCertificateByIDWrapper(id int) (Certificate, error) {
	if err := OpenDatabase(); err != nil {
		return Certificate{}, err
	}
	return Interface.GetCertificate(id)
}

// GetCertificateWrapper 通过域名获取指定证书
func GetCertificateWrapper(domain string) (Certificate, error) {
	if err := OpenDatabase(); err != nil {
		return Certificate{}, err
	}
	return Interface.GetDomainCertificate(domain)
}

// UpdateCertificateInDBWrapper 更新证书信息
func UpdateCertificateInDBWrapper(cert Certificate) error {
	if err := OpenDatabase(); err != nil {
		return err
	}
	return Interface.UpdateCertificate(cert)
}
