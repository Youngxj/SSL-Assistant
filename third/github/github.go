// Package github 提供 GitHub Releases 查询能力（仅查询与输出下载地址，不自动下载更新）。
package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ReleaseAsset 发布资源（一个下载文件）
type ReleaseAsset struct {
	Name string `json:"name"`                 // 文件名
	URL  string `json:"browser_download_url"` // 浏览器直接下载地址
}

// Release 最新发布信息
type Release struct {
	TagName string         `json:"tag_name"` // 版本号，如 v1.0.1
	Name    string         `json:"name"`     // 发布标题
	HTMLURL string         `json:"html_url"` // 发布页面
	Assets  []ReleaseAsset `json:"assets"`
}

// httpClient 复用连接池，超时 10 秒（网络环境受限时快速失败并提示，不阻塞）
var httpClient = &http.Client{Timeout: 10 * time.Second}

// DefaultRepo 本项目仓库
const DefaultRepo = "Youngxj/SSL-Assistant"

// DownloadPage 返回 Releases 下载页面地址（任何情况下都能打印，供用户手动访问）
func DownloadPage(repo string) string {
	return fmt.Sprintf("https://github.com/%s/releases/latest", repo)
}

// apiBaseURL GitHub API 基础地址（测试时可替换为本地 mock）
var apiBaseURL = "https://api.github.com"

// LatestRelease 查询指定仓库的最新 Release。
// 网络不可达/API 限流时返回错误，调用方应提示用户手动访问 DownloadPage。
func LatestRelease(repo string) (*Release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", apiBaseURL, repo)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}
	// GitHub API 要求 User-Agent，缺失会返回 403
	req.Header.Set("User-Agent", "SSL-Assistant")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法访问 GitHub（网络受限或代理问题）: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("仓库 %s 暂无 Release 发布", repo)
	}
	if resp.StatusCode != http.StatusOK {
		// 403 通常是 API 未认证限流，提示手动访问
		return nil, fmt.Errorf("GitHub API 返回 HTTP %d（可能触发未认证限流，请稍后再试或手动访问下载页面）", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %v", err)
	}

	var release Release
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}
	if release.TagName == "" {
		return nil, fmt.Errorf("响应中未找到版本信息")
	}
	return &release, nil
}

// CompareVersions 比较两个语义化版本号（支持 v 前缀）。
// 返回 1 表示 v1 > v2，-1 表示 v1 < v2，0 表示相等。
// 无法解析的段按 0 处理。
func CompareVersions(v1, v2 string) int {
	a := parseVersion(v1)
	b := parseVersion(v2)
	for i := 0; i < 3; i++ {
		if a[i] > b[i] {
			return 1
		}
		if a[i] < b[i] {
			return -1
		}
	}
	return 0
}

// parseVersion 将版本号解析为 [major, minor, patch]（剥离 v 前缀，忽略预发布后缀）
func parseVersion(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	// 去掉预发布/构建元数据后缀（如 -beta.1、+build）
	if idx := strings.IndexAny(v, "-+"); idx >= 0 {
		v = v[:idx]
	}
	parts := strings.Split(v, ".")
	var nums [3]int
	for i := 0; i < 3 && i < len(parts); i++ {
		n, err := strconv.Atoi(strings.TrimSpace(parts[i]))
		if err == nil {
			nums[i] = n
		}
	}
	return nums
}
