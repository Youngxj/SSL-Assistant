package db

// AddCertificateToDBWrapper 添加证书
func AddCertificateToDBWrapper(cert Certificate) error {
	err := OpenDatabase()
	if err != nil {
		return err
	}
	defer Interface.Close()
	err = Interface.AddCertificate(cert)
	if err != nil {
		return err
	}
	return nil
}

// DeleteCertificateFromDBWrapper 删除证书
func DeleteCertificateFromDBWrapper(id int) error {
	err := OpenDatabase()
	if err != nil {
		return err
	}
	defer Interface.Close()
	err = Interface.DeleteCertificate(id)
	if err != nil {
		return err
	}
	return nil
}

// GetAllCertificatesWrapper 获取所有证书
func GetAllCertificatesWrapper() ([]Certificate, error) {
	err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer Interface.Close()
	certs, err := Interface.GetAllCertificates()
	if err != nil {
		return nil, err
	}
	return certs, nil
}

// GetCertificateWrapper 获取指定证书
func GetCertificateWrapper(domain string) (Certificate, error) {
	err := OpenDatabase()
	if err != nil {
		return Certificate{}, nil
	}
	defer Interface.Close()
	cert, err := Interface.GetDomainCertificate(domain)
	if err != nil {
		return cert, err
	}
	return cert, nil
}

// UpdateCertificateInDBWrapper 更新证书信息
func UpdateCertificateInDBWrapper(cert Certificate) error {
	err := OpenDatabase()
	if err != nil {
		return err
	}
	defer Interface.Close()
	err = Interface.UpdateCertificate(cert)
	if err != nil {
		return err
	}
	return nil
}
