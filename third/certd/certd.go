package certd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/fatih/color"
	"io"
	"net/http"
	"ssl_assistant/config"
	"ssl_assistant/utils"
	"strconv"
	"strings"
	"time"
)

// 业务错误码（参考 https://s.apifox.cn/2e76f8c4-7c58-413b-a32d-a1316529af44/254949529e0）
const (
	CodeOK           = 0     // 成功
	CodeCertApplying = 20013 // 已自动触发证书申请，可稍后重新获取
)

// ErrCertApplying 证书申请中（code=20013），上层可提示稍后重试
var ErrCertApplying = errors.New("证书申请中")

// CertDetail 证书详情（响应 data.detail）
type CertDetail struct {
	ID       int      `json:"id"`       // 证书仓库记录ID
	Domains  []string `json:"domains"`  // 域名列表
	NotAfter int64    `json:"notAfter"` // 有效期时间戳
}

// ApiResponse API 响应结构体
type ApiResponse struct {
	Code    int    `json:"code"`
	Message string `json:"msg"`
	Data    struct {
		Crt string `json:"crt"` // 全链证书，fullchain,PEM格式
		Key string `json:"key"` // 私钥，PEM格式
		Ic  string `json:"ic"`  // 中间证书，PEM格式
		Oc  string `json:"oc"`  // 单证书，PEM格式，不含证书链
		Pfx string `json:"pfx"` // PFX格式证书，Base64编码
		Der string `json:"der"` // DER格式证书，Base64编码
		Jks string `json:"jks"` // JKS格式证书，Base64编码
		One string `json:"one"` // 一体化证书，crt+key两个字符串拼接的PEM证书
		P7b string `json:"p7b"` // p7b格式证书
	} `json:"data"`
	Detail *CertDetail `json:"detail"` // 证书详情（含ID/域名/有效期）
}

// certGetRequest 请求体（字段含义见文档）
type certGetRequest struct {
	Domains             string           `json:"domains,omitempty"`   // 域名列表（与certId二选一）
	CertID              int              `json:"certId,omitempty"`    // 证书仓库ID（优先于domains）
	Format              string           `json:"format,omitempty"`    // 证书格式，不传返回全部格式
	AutoApply           bool             `json:"autoApply,omitempty"` // 证书不存在时自动创建流水线申请
	AutoApplyTemplateID int              `json:"autoApplyTemplateId,omitempty"`
	AutoApplyParams     *autoApplyParams `json:"autoApplyParams,omitempty"`
}

// autoApplyParams 自动申请参数（仅保留本项目用到的字段）
type autoApplyParams struct {
	RenewDays int `json:"renewDays,omitempty"` // 到期前多少天更新
}

// 包级 http.Client 复用连接池（避免每次请求新建）
var httpClient = &http.Client{Timeout: 15 * time.Second}

// GetCertificateInfo 获取证书信息
// @param domain 域名（与 certID 二选一）
// @param certID 证书仓库ID（优先于域名）
// @return crt 全链证书PEM, key 私钥PEM, detail 证书详情（可能为nil）
func GetCertificateInfo(domain string, certID int) (crt, key []byte, detail *CertDetail, err error) {
	ApiUrl, err := config.GetConfig("third.certd", "api_url")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("获取api_url配置失败: %v", err)
	}
	// 兼容手工修改 conf.ini 时尾部残留 / 或 \ 的情况
	ApiUrl = strings.TrimRight(ApiUrl, `/\`)
	KeyId, err := config.GetConfig("third.certd", "key_id")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("获取key_id配置失败: %v", err)
	}
	KeySecret, err := config.GetConfig("third.certd", "key_secret")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("获取key_secret配置失败: %v", err)
	}
	if strings.TrimSpace(ApiUrl) == "" || strings.TrimSpace(KeyId) == "" || strings.TrimSpace(KeySecret) == "" {
		return nil, nil, nil, fmt.Errorf("Certd配置不完整，请先通过 init 配置 api_url/key_id/key_secret")
	}

	// 构造请求体：只请求 PEM 格式，避免返回 pfx/der/jks 等无用的大字段
	payload := certGetRequest{
		Domains: domain,
		CertID:  certID,
		Format:  "pem",
	}
	if apply, tplID, renewDays := loadAutoApplyConfig(); apply {
		payload.AutoApply = true
		payload.AutoApplyTemplateID = tplID
		payload.AutoApplyParams = &autoApplyParams{RenewDays: renewDays}
	}

	// 计算token
	token := utils.GetEncodeToken(KeyId, KeySecret)
	url := fmt.Sprintf("%s/api/v1/cert/get", ApiUrl)

	// 网络错误/服务端异常时退避重试（最多3次），业务错误不重试
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		crt, key, detail, retriable, reqErr := doRequest(url, token, payload)
		if reqErr == nil {
			return crt, key, detail, nil
		}
		lastErr = reqErr
		if !retriable {
			return nil, nil, nil, reqErr
		}
		if attempt < 3 {
			time.Sleep(time.Duration(attempt) * 300 * time.Millisecond)
		}
	}
	return nil, nil, nil, fmt.Errorf("请求失败（已重试3次）: %v", lastErr)
}

// doRequest 发送一次请求并解析响应；retriable=true 表示可重试（网络错误/服务端异常）
func doRequest(url, token string, payload certGetRequest) (crt, key []byte, detail *CertDetail, retriable bool, err error) {
	apiPostData, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, nil, false, fmt.Errorf("构造请求体失败: %v", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(apiPostData))
	if err != nil {
		return nil, nil, nil, false, fmt.Errorf("创建请求失败: %v", err)
	}
	req.Header.Set("x-certd-token", token)
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, nil, nil, true, fmt.Errorf("发送请求失败: %v", err)
	}
	defer resp.Body.Close()

	// 限制响应大小，防止异常超大响应
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, nil, nil, true, fmt.Errorf("读取响应失败: %v", err)
	}

	// 检查 HTTP 状态码（4xx 为确定性错误不重试；5xx 为服务端异常可重试）
	if resp.StatusCode != http.StatusOK {
		retriable := resp.StatusCode >= 500
		return nil, nil, nil, retriable, fmt.Errorf("接口返回HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var apiResp ApiResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, nil, nil, false, fmt.Errorf("解析响应失败: %v", err)
	}
	if apiResp.Code != CodeOK {
		if apiResp.Code == CodeCertApplying {
			return nil, nil, nil, false, fmt.Errorf("%w（code=%d，%s）", ErrCertApplying, apiResp.Code, apiResp.Message)
		}
		return nil, nil, nil, false, fmt.Errorf("API 返回错误(code=%d): %s", apiResp.Code, apiResp.Message)
	}

	return []byte(apiResp.Data.Crt), []byte(apiResp.Data.Key), apiResp.Detail, false, nil
}

// loadAutoApplyConfig 读取自动申请配置
func loadAutoApplyConfig() (enable bool, templateID int, renewDays int) {
	apply, _ := config.GetConfig("third.certd", "auto_apply")
	enable = apply == "1" || apply == "true"
	tplID, _ := config.GetConfig("third.certd", "auto_apply_template_id")
	templateID, _ = strconv.Atoi(strings.TrimSpace(tplID))
	renew, _ := config.GetConfig("third.certd", "auto_apply_renew_days")
	renewDays, _ = strconv.Atoi(strings.TrimSpace(renew))
	if renewDays <= 0 {
		renewDays = 10 // 与本地默认提前更新天数保持一致
	}
	return
}

// SetConfig Certd配置
func SetConfig() {
	color.Cyan("正在配置Certd相关参数")
	var rootName string = "third.certd"

	// 读取当前配置（未配置时为空），用于输入框预填
	curApi, _ := config.GetConfig(rootName, "api_url")
	curKeyID, _ := config.GetConfig(rootName, "key_id")
	curApply, _ := config.GetConfig(rootName, "auto_apply")
	curTplID, _ := config.GetConfig(rootName, "auto_apply_template_id")
	curRenewDays, _ := config.GetConfig(rootName, "auto_apply_renew_days")
	if curRenewDays == "" {
		curRenewDays = "10"
	}

	var ApiUrl string
	for {
		// 输入ApiUrl（预填当前值）
		ApiUrl = utils.ReadInput("请输入 ApiUrl（例如 http://your-certd-server.com）: ", curApi)
		//判断url是否存在http或者https
		if !strings.HasPrefix(ApiUrl, "http://") && !strings.HasPrefix(ApiUrl, "https://") {
			color.Red("ApiUrl 错误，需包含 http:// or https:// 请重新输入")
			continue
		}
		//去掉结尾的 / 或 \（兼容 https://xxx/、https://xxx\、https://xxx// 等写法）
		ApiUrl = strings.TrimRight(ApiUrl, `/\`)
		break
	}
	err := config.SetConfig(rootName, "api_url", ApiUrl)
	if err != nil {
		color.Red("保存 api_url 失败: %v", err)
		return
	}

	// 输入KeyId（预填当前值）
	KeyId := utils.ReadInput("请输入 KeyId: ", curKeyID)
	err = config.SetConfig(rootName, "key_id", KeyId)
	if err != nil {
		color.Red("保存 key_id 失败: %v", err)
		return
	}

	// 输入KeySecret（不回显，无法预填；当前已有配置时提示留空保留）
	oldSecret, _ := config.GetConfig(rootName, "key_secret")
	if oldSecret != "" {
		color.Yellow("当前已配置 KeySecret（留空则不修改，直接回车跳过）\n")
	}
	KeySecret := utils.ReadPassword("请输入 KeySecret: ")
	if KeySecret == "" && oldSecret != "" {
		// 留空表示保留原 KeySecret，不覆盖
		KeySecret = oldSecret
	}
	err = config.SetConfig(rootName, "key_secret", KeySecret)
	if err != nil {
		color.Red("保存 key_secret 失败: %v", err)
		return
	}

	// 自动申请配置（证书不存在时由certd自动创建流水线申请；预填当前状态）
	applyVal := "0"
	if curApply == "1" || utils.Confirm("证书不存在时是否自动申请（需certd端已配置域名校验）") {
		applyVal = "1"
	}
	err = config.SetConfig(rootName, "auto_apply", applyVal)
	if err != nil {
		color.Red("保存 auto_apply 失败: %v", err)
		return
	}

	tplID := utils.ReadInput("自动申请参数模版ID（可选，留空使用默认）: ", curTplID)
	err = config.SetConfig(rootName, "auto_apply_template_id", tplID)
	if err != nil {
		color.Red("保存 auto_apply_template_id 失败: %v", err)
		return
	}

	renewDays := utils.ReadInput("自动申请提前更新天数（默认10，建议与本地提前更新天数一致）: ", curRenewDays)
	err = config.SetConfig(rootName, "auto_apply_renew_days", renewDays)
	if err != nil {
		color.Red("保存 auto_apply_renew_days 失败: %v", err)
		return
	}
}
