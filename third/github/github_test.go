package github

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const okBody = `{
	"tag_name": "v1.2.3",
	"name": "v1.2.3",
	"html_url": "https://github.com/Youngxj/SSL-Assistant/releases/tag/v1.2.3",
	"assets": [
		{"name": "SSL-Assistant_1.2.3_Windows_x86_64.zip", "browser_download_url": "https://github.com/Youngxj/SSL-Assistant/releases/download/v1.2.3/win.zip"},
		{"name": "SSL-Assistant_1.2.3_Linux_x86_64.tar.gz", "browser_download_url": "https://github.com/Youngxj/SSL-Assistant/releases/download/v1.2.3/linux.tar.gz"}
	]
}`

func TestLatestReleaseOK(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 必须带 User-Agent（GitHub API 要求）
		if r.Header.Get("User-Agent") == "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, okBody)
	}))
	defer ts.Close()

	old := apiBaseURL
	apiBaseURL = ts.URL
	defer func() { apiBaseURL = old }()

	release, err := LatestRelease("Youngxj/SSL-Assistant")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if release.TagName != "v1.2.3" {
		t.Fatalf("TagName 解析错误: %s", release.TagName)
	}
	if len(release.Assets) != 2 {
		t.Fatalf("应解析 2 个资产，实际 %d", len(release.Assets))
	}
	if release.Assets[0].URL == "" || !strings.HasPrefix(release.Assets[0].URL, "https://") {
		t.Fatalf("资产下载地址解析错误: %+v", release.Assets[0])
	}
}

func TestLatestReleaseErrors(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantSubstr string
	}{
		{"404 无发布", http.StatusNotFound, `{"message":"Not Found"}`, "暂无 Release"},
		{"403 限流", http.StatusForbidden, `{"message":"rate limit"}`, "限流"},
		{"500 服务异常", http.StatusInternalServerError, `server error`, "HTTP 500"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			}))
			defer ts.Close()

			old := apiBaseURL
			apiBaseURL = ts.URL
			defer func() { apiBaseURL = old }()

			_, err := LatestRelease("Youngxj/SSL-Assistant")
			if err == nil {
				t.Fatal("应返回错误")
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Fatalf("错误信息应包含 %q，实际: %v", tt.wantSubstr, err)
			}
		})
	}
}

func TestLatestReleaseNetworkError(t *testing.T) {
	// 指向不可达地址：快速失败（连接拒绝）
	old := apiBaseURL
	apiBaseURL = "http://127.0.0.1:1" // 端口 1 通常立即拒绝
	defer func() { apiBaseURL = old }()

	_, err := LatestRelease("Youngxj/SSL-Assistant")
	if err == nil {
		t.Fatal("网络不可达应返回错误")
	}
	if !strings.Contains(err.Error(), "无法访问 GitHub") {
		t.Fatalf("错误信息应包含网络提示，实际: %v", err)
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		v1, v2 string
		want   int
	}{
		{"v1.0.0", "v1.0.0", 0},
		{"1.0.0", "v1.0.0", 0},
		{"v1.0.1", "v1.0.0", 1},
		{"v1.1.0", "v1.0.9", 1},
		{"v2.0.0", "v1.9.9", 1},
		{"v1.0.0", "v1.0.1", -1},
		{"v1.0.0-beta.1", "v1.0.0", 0}, // 预发布后缀忽略
		{"", "v1.0.0", -1},             // 空版本按 0.0.0
		{"v1.a.0", "v1.0.0", 0},        // 非数字段按 0 处理
		{"v1.2.3.4", "v1.2.3", 0},      // 超过 3 段忽略
		{"v1.2", "v1.2.0", 0},          // 缺少的段按 0 补齐
		{"v10.0.0", "v9.0.0", 1},       // 两位数版本
	}
	for _, tt := range tests {
		got := CompareVersions(tt.v1, tt.v2)
		if got != tt.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.v1, tt.v2, got, tt.want)
		}
	}
}
