package certd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"ssl_assistant/config"
	"sync/atomic"
	"testing"
)

// TestMain 切换工作目录到临时目录并初始化配置，避免污染项目 config/conf.ini
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "certd_test")
	if err != nil {
		panic(err)
	}
	oldWd, _ := os.Getwd()
	if err := os.Chdir(tmp); err != nil {
		panic(err)
	}
	if err := config.InitConfig(); err != nil {
		panic(err)
	}

	code := m.Run()

	os.Chdir(oldWd)
	os.RemoveAll(tmp)
	os.Exit(code)
}

// testServer 构造一个可编程的 certd mock 服务
type testServer struct {
	ts        *httptest.Server
	lastBody  []byte
	failCount int32
}

func newTestServer() *testServer {
	s := &testServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/cert/get", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.lastBody = body
		var req struct {
			Domains             string `json:"domains"`
			CertID              int    `json:"certId"`
			Format              string `json:"format"`
			AutoApply           bool   `json:"autoApply"`
			AutoApplyTemplateID int    `json:"autoApplyTemplateId"`
			AutoApplyParams     struct {
				RenewDays int `json:"renewDays"`
			} `json:"autoApplyParams"`
		}
		json.Unmarshal(body, &req)

		switch req.Domains {
		case "applying.com":
			// code=20013：已触发自动申请
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"code":20013,"msg":"已自动触发证书申请"}`)
		case "unauth.com":
			// 401：确定性错误，不应重试
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `unauthorized`)
		case "svc500.com":
			// 5xx：前两次失败，第三次成功（验证重试）
			n := atomic.AddInt32(&s.failCount, 1)
			if n < 3 {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, `server error`)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"code":0,"msg":"ok","data":{"crt":"CRT-%s","key":"KEY"},"detail":{"id":777,"domains":["svc500.com","www.svc500.com"],"notAfter":2000000000}}`, req.Domains)
		default:
			// 正常返回 + detail
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"code":0,"msg":"ok","data":{"crt":"CRT-%s","key":"KEY"},"detail":{"id":123,"domains":["%s","www.%s"],"notAfter":2000000000}}`, req.Domains, req.Domains, req.Domains)
		}
	})
	s.ts = httptest.NewServer(mux)
	return s
}

func (s *testServer) close() {
	s.ts.Close()
}

func (s *testServer) setup(t *testing.T) {
	t.Helper()
	config.SetConfig("third.certd", "api_url", s.ts.URL)
	config.SetConfig("third.certd", "key_id", "test-key-id")
	config.SetConfig("third.certd", "key_secret", "test-key-secret")
	config.SetConfig("third.certd", "auto_apply", "0")
	config.SetConfig("third.certd", "auto_apply_template_id", "")
	config.SetConfig("third.certd", "auto_apply_renew_days", "")
}

func TestGetCertificateInfoBasic(t *testing.T) {
	s := newTestServer()
	defer s.close()
	s.setup(t)

	// 域名查询：format=pem、detail 解析
	crt, key, detail, err := GetCertificateInfo("example.com", 0)
	if err != nil {
		t.Fatalf("获取证书失败: %v", err)
	}
	if string(crt) != "CRT-example.com" || string(key) != "KEY" {
		t.Fatalf("crt/key 解析错误: %s / %s", crt, key)
	}
	if detail == nil || detail.ID != 123 || detail.NotAfter != 2000000000 {
		t.Fatalf("detail 解析错误: %+v", detail)
	}
	if len(detail.Domains) != 2 || detail.Domains[1] != "www.example.com" {
		t.Fatalf("detail.Domains 解析错误: %v", detail.Domains)
	}

	// 请求体必须只请求 PEM 格式
	var body map[string]interface{}
	json.Unmarshal(s.lastBody, &body)
	if body["format"] != "pem" {
		t.Fatalf("请求体应含 format=pem，实际: %v", body)
	}
	// 未开启 autoApply 时请求体不应带 autoApply 字段
	if _, ok := body["autoApply"]; ok {
		t.Fatalf("未开启 autoApply 时请求体不应含 autoApply 字段: %v", body)
	}
}

func TestGetCertificateInfoCertIDPriority(t *testing.T) {
	s := newTestServer()
	defer s.close()
	s.setup(t)

	// 传 certId 时 domains 应省略
	if _, _, _, err := GetCertificateInfo("", 555); err != nil {
		t.Fatalf("certId 查询失败: %v", err)
	}
	var body map[string]interface{}
	json.Unmarshal(s.lastBody, &body)
	if body["certId"] != float64(555) {
		t.Fatalf("请求体应含 certId=555，实际: %v", body)
	}
	if _, ok := body["domains"]; ok {
		t.Fatalf("certId 优先时不应传 domains: %v", body)
	}
}

func TestGetCertificateInfoAutoApply(t *testing.T) {
	s := newTestServer()
	defer s.close()
	s.setup(t)

	config.SetConfig("third.certd", "auto_apply", "1")
	config.SetConfig("third.certd", "auto_apply_template_id", "9")
	config.SetConfig("third.certd", "auto_apply_renew_days", "23")

	if _, _, _, err := GetCertificateInfo("auto.com", 0); err != nil {
		t.Fatalf("autoApply 请求失败: %v", err)
	}
	var body map[string]interface{}
	json.Unmarshal(s.lastBody, &body)
	if body["autoApply"] != true {
		t.Fatalf("请求体应含 autoApply=true，实际: %v", body)
	}
	if body["autoApplyTemplateId"] != float64(9) {
		t.Fatalf("请求体应含 autoApplyTemplateId=9，实际: %v", body)
	}
	params, ok := body["autoApplyParams"].(map[string]interface{})
	if !ok || params["renewDays"] != float64(23) {
		t.Fatalf("请求体 autoApplyParams.renewDays 应为 23，实际: %v", body)
	}
}

func TestGetCertificateInfoApplying(t *testing.T) {
	s := newTestServer()
	defer s.close()
	s.setup(t)

	// code=20013 → ErrCertApplying
	if _, _, _, err := GetCertificateInfo("applying.com", 0); !errors.Is(err, ErrCertApplying) {
		t.Fatalf("20013 应返回 ErrCertApplying，实际: %v", err)
	}
}

func TestGetCertificateInfoRetry(t *testing.T) {
	s := newTestServer()
	defer s.close()
	s.setup(t)

	// 5xx：重试 3 次后成功
	atomic.StoreInt32(&s.failCount, 0)
	crt, _, detail, err := GetCertificateInfo("svc500.com", 0)
	if err != nil {
		t.Fatalf("5xx 重试后应成功，实际: %v", err)
	}
	if string(crt) != "CRT-svc500.com" || detail.ID != 777 {
		t.Fatalf("重试成功后响应解析错误: crt=%s detail=%+v", crt, detail)
	}
	if atomic.LoadInt32(&s.failCount) != 3 {
		t.Fatalf("应请求 3 次（2 失败+1 成功），实际 %d", atomic.LoadInt32(&s.failCount))
	}

	// 4xx：不应重试，立即失败
	atomic.StoreInt32(&s.failCount, 0)
	if _, _, _, err := GetCertificateInfo("unauth.com", 0); err == nil {
		t.Fatal("401 应失败")
	}
	if atomic.LoadInt32(&s.failCount) != 0 {
		t.Fatalf("4xx 不应重试，实际请求 %d 次", atomic.LoadInt32(&s.failCount))
	}
}

func TestGetCertificateInfoMissingConfig(t *testing.T) {
	// 清空配置应报"配置不完整"；完成后恢复原值，避免影响其他测试（-shuffle 安全）
	config.SetConfig("third.certd", "api_url", "")
	config.SetConfig("third.certd", "key_id", "")
	config.SetConfig("third.certd", "key_secret", "")
	defer func() {
		config.SetConfig("third.certd", "api_url", "http://restored.local")
		config.SetConfig("third.certd", "key_id", "restored-id")
		config.SetConfig("third.certd", "key_secret", "restored-secret")
	}()

	if _, _, _, err := GetCertificateInfo("x.com", 0); err == nil {
		t.Fatal("配置不完整时应报错")
	}
}
