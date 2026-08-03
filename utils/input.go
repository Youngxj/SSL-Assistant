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

// activeCanvas 交互菜单模式下注册的全局画布（由 runInteractiveMenu 设置）。
// SelectMenu/MultiSelectCheckbox 检测到画布激活时改用画布版渲染（分区常驻），
// 非交互 CLI 模式永不设置，行为不变。
var activeCanvas *Canvas

// SetActiveCanvas 注册/清除全局画布。cv 为 nil 时清除（退出交互菜单）。
func SetActiveCanvas(cv *Canvas) { activeCanvas = cv }

// exitWithCleanup 退出前清理画布（恢复终端/结束输出重定向），
// 避免 ReadInput EOF 或 Ctrl+C 直接 os.Exit 时终端残留画布画面。
func exitWithCleanup(code int) {
	if activeCanvas != nil {
		activeCanvas.Restore()
		activeCanvas = nil
	}
	os.Exit(code)
}

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
		exitWithCleanup(1)
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return def
	}
	return input
}

// SelectMenu 终端下方向键单选菜单（↑/↓ 移动、回车确认、ESC 取消），非终端回退序号输入。
// 交互菜单（画布激活）时渲染到画布辅区域，仅重绘菜单区；否则流式渲染。
// 返回选中项索引（0 起）；取消/EOF 返回 -1。
func SelectMenu(items []string, prompt string) int {
	if len(items) == 0 {
		return -1
	}
	if !IsInteractive() {
		return selectMenuNumeric(items, prompt)
	}
	if activeCanvas != nil {
		return selectMenuKeyNavOnCanvas(activeCanvas, items, prompt)
	}
	return selectMenuKeyNav(items, prompt)
}

// SelectMenuOnCanvas 画布版单选菜单（显式指定画布）：渲染在辅区域（菜单区），
// 方向键移动时仅重绘菜单区高亮，不重绘列表区与反馈区。非终端环境回退序号输入。
func SelectMenuOnCanvas(cv *Canvas, items []string, prompt string) int {
	if len(items) == 0 {
		return -1
	}
	if !IsInteractive() {
		return selectMenuNumeric(items, prompt)
	}
	return selectMenuKeyNavOnCanvas(cv, items, prompt)
}

// selectMenuKeyNavOnCanvas 画布版方向键平铺菜单：复用 selectMenuKeyNav 的交互逻辑，
// 但渲染调用 cv.DrawMenu（绝对定位到菜单区），移动时只重绘菜单区。
func selectMenuKeyNavOnCanvas(cv *Canvas, items []string, prompt string) int {
	if prompt == "" {
		prompt = "←/→ 移动，回车确认，ESC 取消: "
	}
	cur := 0
	fd := int(os.Stdin.Fd())

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return selectMenuNumeric(items, prompt)
	}
	defer term.Restore(fd, oldState)
	// Windows 控制台需启用虚拟终端输入，方向键才会产生可读字节流
	_ = enableVTInput(fd)

	// 隐藏光标，退出时恢复（画布渲染走原始终端句柄）
	fmt.Fprint(cv.termOut, "\x1b[?25l")
	defer fmt.Fprint(cv.termOut, "\x1b[?25h")

	termWidth := 80
	if w, _, err := term.GetSize(fd); err == nil && w > 0 {
		termWidth = w
	}
	cols, _, _ := menuGridSize(items, termWidth)

	// render 仅重绘菜单区（绝对定位由 cv.DrawMenu 完成）
	render := func() {
		cv.DrawMenu(items, cur)
	}

	render()
	for {
		key := readRawKey()
		if key == nil {
			return -1
		}
		switch {
		case key[0] == '\r' || key[0] == '\n':
			// 回车确认当前项
			fmt.Fprint(cv.termOut, "\r\n")
			return cur
		case key[0] == 0x03:
			// Ctrl+C：恢复终端与光标（os.Exit 会跳过 defer）并中断
			fmt.Fprint(cv.termOut, "\x1b[?25h\r\n")
			term.Restore(fd, oldState)
			exitWithCleanup(130)
		case len(key) >= 3 && key[0] == 0x1b && key[1] == '[':
			// 方向键（←/→ 左右移动，↑/↓ 上下换行）
			switch key[2] {
			case keyLeft: // ←
				if cur > 0 {
					cur--
					render()
				}
			case keyRight: // →
				if cur < len(items)-1 {
					cur++
					render()
				}
			case keyUp: // ↑
				if cur >= cols {
					cur -= cols
					render()
				}
			case keyDown: // ↓
				if cur+cols < len(items) {
					cur += cols
					render()
				}
			}
		case key[0] == 0x1b:
			// 单独的 ESC：取消
			fmt.Fprint(cv.termOut, "\r\n")
			return -1
		}
	}
}

// selectMenuNumeric 序号输入模式（非终端回退）：输入序号回车选择，直接回车默认第一项。
func selectMenuNumeric(items []string, prompt string) int {
	_ = prompt // 非终端场景使用序号提示（不展示方向键文案）
	menuPrompt := "输入序号选择（直接回车默认第一项）: "
	for {
		input := ReadInput(menuPrompt, "1")
		idx, err := strconv.Atoi(strings.TrimSpace(input))
		if err == nil && idx >= 1 && idx <= len(items) {
			return idx - 1
		}
		color.Red("无效序号，请输入 1-%d\n", len(items))
	}
}

// readRawKey 读取原始终端的一个按键。
// 方向键为 ESC 序列（如 \x1b[A），补齐读取后续字节；兼容 VT 模式（\x1b[A）与应用模式（\x1bOA，归一为 \x1b[A）。
// 读取失败返回 nil。
func readRawKey() []byte {
	one := make([]byte, 1)
	if _, err := os.Stdin.Read(one); err != nil {
		return nil
	}
	if one[0] != 0x1b {
		return one
	}
	// ESC 序列：读后续字节（最多 2 个，如 [A 或 OA）
	rest := make([]byte, 2)
	n := 0
	for n < 2 {
		m, err := os.Stdin.Read(rest[n:])
		if err != nil {
			break
		}
		n += m
	}
	// 应用模式 \x1bOA/B/C/D → 归一为 \x1b[A/B/C/D，统一方向键处理
	if n >= 2 && rest[0] == 'O' && (rest[1] == 'A' || rest[1] == 'B' || rest[1] == 'C' || rest[1] == 'D') {
		rest[0] = '['
	}
	return append(one, rest[:n]...)
}

// 方向键序列的第三个字节（归一后）
const (
	keyUp    = 'A' // ↑
	keyDown  = 'B' // ↓
	keyRight = 'C' // →
	keyLeft  = 'D' // ←
)

// displayWidth 计算字符串在终端中的显示宽度（CJK 全角字符按 2 列计），用于平铺对齐。
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		switch {
		case r == 0: // 忽略空字符
		case r >= 0x1100 && (r <= 0x115F || r == 0x2329 || r == 0x232A ||
			(r >= 0x2E80 && r <= 0xA4CF && r != 0x303F) ||
			(r >= 0xAC00 && r <= 0xD7A3) ||
			(r >= 0xF900 && r <= 0xFAFF) ||
			(r >= 0xFE30 && r <= 0xFE4F) ||
			(r >= 0xFF00 && r <= 0xFF60) ||
			(r >= 0xFFE0 && r <= 0xFFE6)):
			w += 2
		default:
			w++
		}
	}
	return w
}

// menuGridCols 计算平铺列数：每项实际占用 itemWidth+1 列（含项间 1 空格），
// 保证整行不超出终端宽度；至少 1 列，最多不超过项数。
func menuGridCols(termWidth, itemWidth, n int) int {
	if termWidth <= 0 || itemWidth <= 0 || n <= 0 {
		return 1
	}
	cols := (termWidth + 1) / (itemWidth + 1)
	if cols < 1 {
		cols = 1
	}
	if cols > n {
		cols = n
	}
	return cols
}

// menuGridSize 计算平铺网格的列数、行数与单列宽度（供菜单渲染与画布共用）。
func menuGridSize(items []string, termWidth int) (cols, rows, itemWidth int) {
	if len(items) == 0 {
		return 1, 0, 0
	}
	maxW := 0
	for _, it := range items {
		if dw := displayWidth(fmt.Sprintf("%2d. %s", len(items), it)); dw > maxW {
			maxW = dw
		}
	}
	itemWidth = maxW + 1 // 项间距（紧凑，每行尽量多放）
	cols = menuGridCols(termWidth, itemWidth, len(items))
	rows = (len(items) + cols - 1) / cols
	return cols, rows, itemWidth
}

// selectMenuKeyNav 方向键平铺菜单：菜单项按终端宽度横向平铺（←/→ 左右移动、↑/↓ 上下换行），
// 回车确认，ESC 取消。需要原始终端输入；MakeRaw 失败时回退序号输入。
func selectMenuKeyNav(items []string, prompt string) int {
	if prompt == "" {
		prompt = "←/→ 移动，回车确认，ESC 取消: "
	}
	cur := 0
	fd := int(os.Stdin.Fd())

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return selectMenuNumeric(items, prompt)
	}
	defer term.Restore(fd, oldState)
	// Windows 控制台需启用虚拟终端输入，方向键才会产生可读字节流
	_ = enableVTInput(fd)

	// 隐藏光标，退出时恢复
	fmt.Print("\x1b[?25l")
	defer fmt.Print("\x1b[?25h")

	// 平铺列数：按终端宽度与最长菜单项宽度估算（至少 1 列，最多等于项数）。
	// GetSize 失败（双击启动/git-bash 伪终端/无控制台句柄等）时回退默认 80 列，
	// 避免退化为单列竖排。
	termWidth := 80 // 默认终端宽度
	if w, _, err := term.GetSize(fd); err == nil && w > 0 {
		termWidth = w
	}
	cols, rows, itemWidth := menuGridSize(items, termWidth)

	// render 重绘整个菜单区域（平铺网格 + 提示行）
	render := func(first bool) {
		if !first {
			// CSI n F：上移 n 行；\r 回到行首（xterm 系终端上移保持列位置，需显式回车）
			fmt.Printf("\x1b[%dF\r", rows)
		}
		for r := 0; r < rows; r++ {
			for c := 0; c < cols; c++ {
				i := r*cols + c
				if i >= len(items) {
					break
				}
				line := fmt.Sprintf("%2d. %s", i+1, items[i])
				if pad := itemWidth - displayWidth(line); pad > 0 {
					line += strings.Repeat(" ", pad)
				}
				if i == cur {
					fmt.Print(color.New(color.ReverseVideo).Sprint(line))
				} else {
					fmt.Print(line)
				}
				if c < cols-1 && i < len(items)-1 {
					fmt.Print(" ")
				}
			}
			fmt.Print("\x1b[K\r\n") // 行尾清行并显式回车换行（raw 模式下 \n 不会自动 \r）
		}
		fmt.Printf("\x1b[K%s", prompt)
	}

	render(true)
	for {
		key := readRawKey()
		if key == nil {
			return -1
		}
		switch {
		case key[0] == '\r' || key[0] == '\n':
			// 回车确认当前项
			fmt.Print("\r\n")
			return cur
		case key[0] == 0x03:
			// Ctrl+C：恢复终端与光标（os.Exit 会跳过 defer）并中断
			fmt.Print("\x1b[?25h\r\n")
			term.Restore(fd, oldState)
			exitWithCleanup(130)
		case len(key) >= 3 && key[0] == 0x1b && key[1] == '[':
			// 方向键（←/→ 左右移动，↑/↓ 上下换行）
			switch key[2] {
			case keyLeft: // ←
				if cur > 0 {
					cur--
					render(false)
				}
			case keyRight: // →
				if cur < len(items)-1 {
					cur++
					render(false)
				}
			case keyUp: // ↑
				if cur >= cols {
					cur -= cols
					render(false)
				}
			case keyDown: // ↓
				if cur+cols < len(items) {
					cur += cols
					render(false)
				}
			}
		case key[0] == 0x1b:
			// 单独的 ESC：取消
			fmt.Print("\r\n")
			return -1
		}
	}
}

// MultiSelectCheckbox 多选勾选列表。
// 终端环境下为方向键交互：↑/↓ 移动高亮，空格切换勾选，回车确认（ESC 取消）；
// 非终端（管道/重定向）回退为序号输入：输入序号（空格分隔可一次切换多个）后回车，直接回车确认。
// 交互菜单（画布激活）时渲染到画布主区域（列表区）并随高亮滚动，其余区域不受影响。
// 返回已勾选项的下标（与 items 顺序一致）；items 为空时返回空切片。
func MultiSelectCheckbox(items []string, prompt string) []int {
	if len(items) == 0 {
		return nil
	}
	if !IsInteractive() {
		return multiSelectNumeric(items, prompt)
	}
	if activeCanvas != nil {
		return multiSelectKeyNavOnCanvas(activeCanvas, items, prompt)
	}
	return multiSelectKeyNav(items, prompt)
}

// adjustWindow 计算滚动窗口起始行：确保高亮 cur 始终位于窗口 [winStart, winStart+visible) 内。
// 返回新的 winStart。
func adjustWindow(winStart, cur, visible int) int {
	if cur < winStart {
		winStart = cur
	}
	if cur >= winStart+visible {
		winStart = cur - visible + 1
	}
	return winStart
}

// multiSelectKeyNavOnCanvas 画布版多选勾选：列表渲染在画布主区域（列表区），
// 以当前高亮为中心的窗口滚动显示（窗口高度 = 列表区行数）；方向键移动、
// 空格勾选仅重绘列表区，菜单区与反馈区不受影响。提示写入反馈区。
func multiSelectKeyNavOnCanvas(cv *Canvas, items []string, prompt string) []int {
	if prompt == "" {
		prompt = "↑/↓ 移动，空格勾选，回车确认，ESC 取消: "
	}
	selected := make([]bool, len(items))
	cur := 0
	winStart := 0
	visible := cv.listH
	if visible > len(items) {
		visible = len(items)
	}
	fd := int(os.Stdin.Fd())

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return multiSelectNumeric(items, prompt)
	}
	defer term.Restore(fd, oldState)
	// Windows 控制台需启用虚拟终端输入，方向键才会产生可读字节流
	_ = enableVTInput(fd)

	// 隐藏光标，退出时恢复（画布渲染走原始终端句柄）
	fmt.Fprint(cv.termOut, "\x1b[?25l")
	defer fmt.Fprint(cv.termOut, "\x1b[?25h")

	// render 仅重绘列表区（窗口随高亮滚动），提示写入反馈区
	render := func() {
		winStart = adjustWindow(winStart, cur, visible)
		fmt.Fprintf(cv.termOut, "\x1b[%d;1H", cv.listTop)
		for i := 0; i < cv.listH; i++ {
			idx := winStart + i
			var line string
			if idx < len(items) {
				mark := " "
				if selected[idx] {
					mark = "x"
				}
				line = fmt.Sprintf("[%s] %d. %s", mark, idx+1, items[idx])
				if idx == cur {
					line = "> " + color.New(color.ReverseVideo).Sprint(line)
				} else {
					line = "  " + line
				}
			}
			if dw := displayWidth(line); dw > cv.width {
				line = truncateWidth(line, cv.width)
			}
			fmt.Fprint(cv.termOut, "\x1b[K")
			if line != "" {
				fmt.Fprint(cv.termOut, line)
			}
			fmt.Fprint(cv.termOut, "\r\n")
		}
		// 提示写入反馈区
		cv.ResetFeedback()
		cv.Write([]byte(prompt + "\n"))
	}

	render()
	for {
		key := readRawKey()
		if key == nil {
			break
		}
		switch {
		case key[0] == '\r' || key[0] == '\n':
			// 回车确认
			fmt.Fprint(cv.termOut, "\r\n")
			return collectSelected(selected)
		case key[0] == 0x03:
			// Ctrl+C：恢复终端与光标（os.Exit 会跳过 defer）并中断
			fmt.Fprint(cv.termOut, "\x1b[?25h\r\n")
			term.Restore(fd, oldState)
			exitWithCleanup(130)
		case key[0] == ' ':
			// 空格切换当前项勾选
			selected[cur] = !selected[cur]
			render()
		case len(key) >= 3 && key[0] == 0x1b && key[1] == '[':
			// 方向键（↑/↓/←/→ 均移动高亮）
			switch key[2] {
			case keyUp, keyLeft:
				cur--
				if cur < 0 {
					cur = len(items) - 1
				}
				render()
			case keyDown, keyRight:
				cur++
				if cur >= len(items) {
					cur = 0
				}
				render()
			}
		case len(key) == 1 && key[0] == 0x1b:
			// ESC 单独按下：取消选择
			fmt.Fprint(cv.termOut, "\r\n")
			return nil
		}
	}
	fmt.Fprint(cv.termOut, "\r\n")
	return collectSelected(selected)
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
	// Windows 控制台需启用虚拟终端输入，方向键才会产生可读字节流
	_ = enableVTInput(fd)

	// 隐藏光标，退出时恢复
	fmt.Print("\x1b[?25l")
	defer fmt.Print("\x1b[?25h")

	// render 重绘整个列表区域（列表 + 提示行）；first=true 时不移动光标（首次打印）
	render := func(first bool) {
		if !first {
			// CSI n F：上移 n 行；\r 回到行首（xterm 系终端上移保持列位置，需显式回车）
			fmt.Printf("\x1b[%dF\r", len(items))
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

	render(true)
	for {
		key := readRawKey()
		if key == nil {
			break
		}
		switch {
		case key[0] == '\r' || key[0] == '\n':
			// 回车确认
			fmt.Print("\r\n")
			return collectSelected(selected)
		case key[0] == 0x03:
			// Ctrl+C：恢复终端与光标（os.Exit 会跳过 defer）并中断
			fmt.Print("\x1b[?25h\r\n")
			term.Restore(fd, oldState)
			exitWithCleanup(130)
		case key[0] == ' ':
			// 空格切换当前项勾选
			selected[cur] = !selected[cur]
			render(false)
		case len(key) >= 3 && key[0] == 0x1b && key[1] == '[':
			// 方向键（↑/↓/←/→ 均移动高亮）
			switch key[2] {
			case keyUp, keyLeft:
				cur--
				if cur < 0 {
					cur = len(items) - 1
				}
				render(false)
			case keyDown, keyRight:
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
		exitWithCleanup(1)
	}
	// 非终端环境回退明文读取
	input, err := inputReader().ReadString('\n')
	if err != nil {
		fmt.Println()
		exitWithCleanup(1)
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
