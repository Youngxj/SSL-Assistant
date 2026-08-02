package utils

import (
	"bufio"
	"fmt"
	"github.com/fatih/color"
	"os"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// stdinReader 全局共享的 stdin 读取器。
// 必须共享而非每次新建：bufio.Reader 会预读缓冲，新建会丢弃已被前一次读入缓冲的数据（管道多行输入场景）。
var stdinReader *bufio.Reader

func inputReader() *bufio.Reader {
	if stdinReader == nil {
		stdinReader = bufio.NewReader(os.Stdin)
	}
	return stdinReader
}

// IsInteractive 判断标准输入是否为交互终端。
// Windows 下基于 GetConsoleMode（cmd/PowerShell/Windows Terminal 均正确）；
// git-bash/MSYS2 等伪终端无法可靠检测，可设置环境变量 SSL_ASSISTANT_INTERACTIVE=1 强制视为交互。
func IsInteractive() bool {
	if os.Getenv("SSL_ASSISTANT_INTERACTIVE") == "1" {
		return true
	}
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// ReadInput 打印提示并读取一行输入（去除首尾空白）。
// 输入为空时返回 def；读到 EOF（如管道关闭/非交互误跑）时以非零退出码结束，避免空输入继续走业务逻辑。
func ReadInput(prompt, def string) string {
	fmt.Print(prompt)
	input, err := inputReader().ReadString('\n')
	if err != nil {
		// EOF 或读取失败：视为用户中断/非交互环境，非零退出避免被脚本误判为成功
		fmt.Println()
		os.Exit(1)
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return def
	}
	return input
}

// MultiSelectCheckbox 多选勾选列表。
// 终端环境下为方向键交互：↑/↓ 移动高亮，空格切换勾选，回车确认（ESC 取消）；
// 非终端（管道/重定向）回退为序号输入：输入序号（空格分隔可一次切换多个）后回车，直接回车确认。
// 返回已勾选项的下标（与 items 顺序一致）；items 为空时返回空切片。
func MultiSelectCheckbox(items []string, prompt string) []int {
	if len(items) == 0 {
		return nil
	}
	if IsInteractive() {
		return multiSelectKeyNav(items, prompt)
	}
	return multiSelectNumeric(items, prompt)
}

// multiSelectNumeric 序号输入模式（非终端回退）：输入序号切换勾选，空行确认。
func multiSelectNumeric(items []string, prompt string) []int {
	if prompt == "" {
		prompt = "输入序号切换勾选（多个用空格分隔，直接回车确认）: "
	}
	selected := make([]bool, len(items))
	for {
		fmt.Println()
		for i, item := range items {
			mark := " "
			if selected[i] {
				mark = "x"
			}
			fmt.Printf("[%s] %d. %s\n", mark, i+1, item)
		}
		input := ReadInput(prompt, "")
		if input == "" {
			break
		}
		valid := false
		for _, f := range strings.Fields(input) {
			n, err := strconv.Atoi(f)
			if err != nil || n < 1 || n > len(items) {
				color.Yellow("无效序号: %s（范围 1-%d）\n", f, len(items))
				continue
			}
			valid = true
			selected[n-1] = !selected[n-1]
		}
		if !valid {
			color.Yellow("请输入 1-%d 之间的序号\n", len(items))
		}
	}
	return collectSelected(selected)
}

// multiSelectKeyNav 方向键交互模式：↑/↓ 移动高亮，空格切换勾选，回车确认，ESC 取消。
// 需要原始终端输入；MakeRaw 失败时回退序号输入。
func multiSelectKeyNav(items []string, prompt string) []int {
	if prompt == "" {
		prompt = "↑/↓ 移动，空格勾选，回车确认，ESC 取消: "
	}
	selected := make([]bool, len(items))
	cur := 0
	fd := int(os.Stdin.Fd())

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		// 无法进入原始模式（如非终端）时回退序号输入
		return multiSelectNumeric(items, prompt)
	}
	defer term.Restore(fd, oldState)

	// 隐藏光标，退出时恢复
	fmt.Print("\x1b[?25l")
	defer fmt.Print("\x1b[?25h")

	// render 重绘整个列表区域（列表 + 提示行）；first=true 时不移动光标（首次打印）
	render := func(first bool) {
		if !first {
			// CSI n F：上移 n 行并回到行首（n = 列表行数，光标当前在提示行，回到列表第一行）
			fmt.Printf("\x1b[%dF", len(items))
		}
		for i, item := range items {
			mark := " "
			if selected[i] {
				mark = "x"
			}
			line := fmt.Sprintf("[%s] %d. %s", mark, i+1, item)
			if i == cur {
				fmt.Print("> " + color.New(color.ReverseVideo).Sprint(line))
			} else {
				fmt.Print("  " + line)
			}
			fmt.Print("\x1b[K\r\n") // 清行并显式回车换行（raw 模式下 \n 不会自动 \r）
		}
		fmt.Printf("\x1b[K%s", prompt)
	}

	// readKey 读取一个按键；方向键等 ESC 序列（\x1b[A）补齐读取
	readKey := func() []byte {
		one := make([]byte, 1)
		if _, err := os.Stdin.Read(one); err != nil {
			return nil
		}
		if one[0] != 0x1b {
			return one
		}
		rest := make([]byte, 2)
		n := 0
		for n < 2 {
			m, err := os.Stdin.Read(rest[n:])
			if err != nil {
				break
			}
			n += m
		}
		return append(one, rest[:n]...)
	}

	render(true)
	for {
		key := readKey()
		if key == nil {
			break
		}
		switch {
		case key[0] == '\r' || key[0] == '\n':
			// 回车确认
			fmt.Print("\r\n")
			return collectSelected(selected)
		case key[0] == ' ':
			// 空格切换当前项勾选
			selected[cur] = !selected[cur]
			render(false)
		case len(key) >= 3 && key[0] == 0x1b && key[1] == '[':
			switch key[2] {
			case 'A': // ↑
				cur--
				if cur < 0 {
					cur = len(items) - 1
				}
				render(false)
			case 'B': // ↓
				cur++
				if cur >= len(items) {
					cur = 0
				}
				render(false)
			}
		case len(key) == 1 && key[0] == 0x1b:
			// ESC 单独按下：取消选择
			fmt.Print("\r\n")
			return nil
		}
	}
	fmt.Print("\r\n")
	return collectSelected(selected)
}

// collectSelected 收集勾选下标
func collectSelected(selected []bool) []int {
	var result []int
	for i, s := range selected {
		if s {
			result = append(result, i)
		}
	}
	return result
}

// Confirm 打印 y/n 确认提示，返回是否确认（y/yes 视为确认，其他视为否）。
func Confirm(prompt string) bool {
	input := ReadInput(prompt+"(y/n): ", "")
	switch strings.ToLower(input) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

// ReadPassword 读取敏感输入（不回显）。
// 非交互终端（如管道）下回退为明文读取，保证可用性；EOF 时退出，避免静默保存空密钥。
func ReadPassword(prompt string) string {
	fmt.Print(prompt)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		raw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err == nil {
			return strings.TrimSpace(string(raw))
		}
		// 读取失败（EOF 等）直接退出，避免保存空值
		fmt.Println()
		os.Exit(1)
	}
	// 非终端环境回退明文读取
	input, err := inputReader().ReadString('\n')
	if err != nil {
		fmt.Println()
		os.Exit(1)
	}
	return strings.TrimSpace(input)
}

// InitConsole 初始化跨平台控制台输出。
// Windows 下将控制台输入/输出代码页切换为 UTF-8（65001），避免中文/emoji 乱码；程序退出后自动恢复。
func InitConsole() {
	if runtime.GOOS != "windows" {
		return
	}
	// 仅在 stdout 为控制台时设置，管道/重定向场景忽略失败
	if term.IsTerminal(int(os.Stdout.Fd())) {
		_ = setConsoleCP(65001)
		_ = setConsoleOutputCP(65001)
	}
}
