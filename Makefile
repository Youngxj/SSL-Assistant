# SSL-Assistant 构建脚本
# 发布版本号由 GoReleaser 在 CI 打 tag 时自动注入（.goreleaser.yaml 的 -X main.Version={{.Version}}）；
# 本地构建通过 make build 自动取 git 版本（tag 或最近提交哈希）注入，避免出现"版本未知"。

VERSION ?= $(shell git describe --tags --always)

.PHONY: build test vet fmt clean

build:
	go build -ldflags "-X main.Version=$(VERSION)" -o ssl_assistant

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

clean:
	rm -f ssl_assistant ssl_assistant.exe
