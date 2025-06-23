# SSL Assistant SSL证书部署管理助手

## 项目简介

SSL Assistant 是一个基于 Go 语言开发的跨平台证书部署管理助手，用于SSL远程证书拉取，并自动完成SSL证书更新及生效流程。该工具支持
Windows 和 Linux 平台，可以自动寻找 Nginx 服务对应站点的配置文件，获取域名和证书信息，并将证书信息保存到数据库中。
可通过计划任务定期更新证书，实现 SSL 证书的自动更新和部署。

## 功能特点

- 跨平台支持：同时支持 Windows 和 Linux 系统
- 自动化管理：自动寻找 Nginx 配置文件（已兼容宝塔面板），获取域名和证书信息
- 证书更新：主动拉取远程证书信息，自动更新证书部署，并执行重载命令
    - [x] [Certd](https://github.com/certd/certd) 流水线申请部署证书工具
    - [ ] 未来将支持更多平台……
- 命令行操作：提供简单易用的命令行界面
- 本地存储：使用 SQLite / BadgerDB 数据库存储证书信息

## 安装与使用

1. 下载对应平台的运行文件 [Releases](https://github.com/Youngxj/SSL-Assistant/releases)
2. 运行程序：`SSL-Assistant init`根据提示完成初始化配置，填写API地址、API密钥、重载命令等信息
3. 添加证书：输入域名，程序会自动根据域名获取证书信息，并将证书信息保存到数据库中，以便后面的更新操作
4. 定期更新：可使用Crontab设置定时任务，自动更新证书

## 开发流程

### 从源码编译

1. 克隆仓库

```bash
git clone https://github.com/Youngxj/SSL-Assistant.git
cd SSL-Assistant
```

2. 编译项目

```bash
go build -o ssl_assistant
```

3. 将可执行文件添加到系统路径

#### Windows

## Windows运行指南

### 系统路径配置

1. 将编译好的SSL-Assistant.exe复制到下列任一目录：
    - `C:\Windows\System32`（需要管理员权限）
    - `%USERPROFILE%\bin`（需手动添加至PATH环境变量）

2. 通过PowerShell永久添加环境变量：

```powershell
[Environment]::SetEnvironmentVariable("Path", "$env:Path;D:\your\build\path", "User")
```

#### Linux

```bash
sudo cp SSL-Assistant /usr/local/bin/
```

## 使用方法

### 初始化

```bash
SSL-Assistant init
```

初始化程序，设置证书信息获取的凭证和证书更新后需要执行的命令。初始化完成后，程序会自动寻找 Nginx 配置文件，获取域名和证书信息。

### 添加证书

```bash
SSL-Assistant add
```

添加证书，输入域名，程序会自动根据域名获取证书信息，并将证书信息保存到数据库中。

### 删除证书

```bash
SSL-Assistant del
```

删除证书，输入证书 ID，程序会自动删除对应的证书信息。

### 查看证书

```bash
SSL-Assistant show
```

查看证书，显示证书信息的表格，包括 ID、域名、状态、创建时间、过期时间、证书路径、私钥路径等信息。

### 更新证书

```bash
SSL-Assistant update
```

更新证书，程序会自动获取所有证书信息，并将证书信息保存到数据库中，更新证书对应域名的证书文件内容，并执行重载命令。

## 计划任务设置

### Windows

1. 打开任务计划程序
2. 创建基本任务
3. 设置触发器为每天或每周
4. 设置操作为启动程序，程序为 `SSL-Assistant`，参数为 `update`

### Linux

使用 crontab 设置定时任务：

```bash
crontab -e
```

添加以下内容：

```
0 0 * * * /usr/local/bin/SSL-Assistant update
```

这将在每天凌晨执行证书更新。

## 配置文件

配置文件存储在用户主目录的 `.ssl_assistant` 文件夹中：

- Windows: `C:\Users\<username>\.ssl_assistant`
- Linux: `/home/<username>/.ssl_assistant`

## 注意事项

1. 确保程序有足够的权限读取 Nginx 配置文件和写入证书文件
2. 证书更新后会自动执行重载命令，请确保命令正确
3. 定期检查证书状态，确保证书有效

## 许可证

MIT