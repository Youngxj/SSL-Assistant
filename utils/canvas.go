package utils

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/fatih/color"
	"golang.org/x/term"
)

// Canvas 交互菜单的常驻分区画布。
//
// 布局（自上而下）：
//
//	row 1           标题（固定）
//	row 2           分隔线
//	row 3..菜单区前  证书列表区（主区域，操作后仅重绘本区）
//	菜单区          操作选项（辅区域，方向键移动仅重绘本区高亮）
//	反馈区          操作输出（每次操作前清空，输出重定向至此）
//
// 所有画布渲染都写入 termOut（构造时保存的原始终端），因此即使 os.Stdout
// 在操作执行期间被重定向，也不会造成循环输出。非交互 CLI 模式
// （init/add/update 等子命令）不经过 Canvas，输出行为不变。
type Canvas struct {
	width, height int
	termOut       *os.File // 原始终端输出句柄（画布渲染专用）

	listTop, listH int // 证书列表区（1-based 行号）
	menuTop, menuH int // 菜单区（含提示行）
	outTop, outH   int // 反馈区

	// 反馈区内容缓冲（最多 outH 行），整区重绘以支持滚动
	outLines []string
	outCur   string

	// 输出捕获期间的状态（BeginOutput/EndOutput）
	savedStdout  *os.File
	savedColor   io.Writer
	outPipeR     *os.File
	outPipeW     *os.File
	outDone      chan struct{}
	outputActive bool
}

// NewCanvas 获取终端尺寸并计算三区布局。GetSize 失败时回退 80x24。
// items 用于计算菜单区所需行数（平铺行数 + 1 提示行）。
func NewCanvas(items []string) *Canvas {
	width, height := 80, 24
	if w, h, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 && h > 0 {
		width, height = w, h
	}
	cv := &Canvas{width: width, height: height, termOut: os.Stdout}
	cv.layout(items)
	return cv
}

// layout 计算各区行号。菜单区固定在底部上方，反馈区占底部 outH 行，
// 列表区占其余空间。小高度终端（height <= 菜单+反馈+标题）时列表区高度可为 0，
// DrawList 会自动跳过，保证区域不重叠。
func (cv *Canvas) layout(items []string) {
	// 菜单平铺行数（与 selectMenuKeyNav 相同的几何计算）
	_, menuRows, _ := menuGridSize(items, cv.width)

	// 反馈区高度：至少 1 行，最多 6 行；菜单区行数 = 平铺行数 + 1 提示行
	outH := 6
	cv.menuH = menuRows + 1
	if cv.menuH+outH+2 > cv.height { // 标题(1)+列表区分隔(1) 之外的空间
		outH = cv.height - cv.menuH - 2
		if outH < 1 {
			outH = 1
		}
	}
	cv.outH = outH
	cv.menuTop = cv.height - cv.menuH - cv.outH + 1
	if cv.menuTop < 1 {
		cv.menuTop = 1
	}
	cv.outTop = cv.menuTop + cv.menuH

	cv.listTop = 3
	cv.listH = cv.menuTop - cv.listTop - 1 // 列表区与菜单区间留 1 行分隔
	if cv.listH < 0 {
		cv.listH = 0
	}
}

// TermSize 返回画布尺寸（供测试与调试）。
func (cv *Canvas) TermSize() (w, h int) { return cv.width, cv.height }

// Init 清屏并绘制标题与分隔线，然后重绘列表区与菜单区。
func (cv *Canvas) Init(listLines []string, items []string, cur int) {
	fmt.Fprint(cv.termOut, "\x1b[2J\x1b[H") // 清屏并回左上角
	fmt.Fprintf(cv.termOut, "\x1b[1;1H\x1b[K")
	color.New(color.FgCyan).Fprint(cv.termOut, "══════ SSL Assistant 操作菜单 ══════")
	fmt.Fprint(cv.termOut, "\r\n")
	cv.DrawList(listLines)
	cv.DrawMenu(items, cur)
}

// Restore 退出画布：恢复被重定向的输出并清屏（保留一行提示）。
func (cv *Canvas) Restore() {
	cv.EndOutput()
	fmt.Fprint(cv.termOut, "\x1b[2J\x1b[H")
	fmt.Fprintln(cv.termOut, "已退出交互菜单")
}

// DrawList 仅重绘证书列表区（主区域），不影响菜单区与反馈区。
// lines 为空时清空该区域；超出区域高度的行被截断；listH 为 0（小终端）时跳过。
func (cv *Canvas) DrawList(lines []string) {
	if cv.listH <= 0 {
		return
	}
	fmt.Fprintf(cv.termOut, "\x1b[%d;1H", cv.listTop)
	for i := 0; i < cv.listH; i++ {
		var line string
		if i < len(lines) {
			line = lines[i]
		}
		if dw := displayWidth(line); dw > cv.width {
			line = truncateWidth(line, cv.width)
		}
		fmt.Fprint(cv.termOut, "\x1b[K")
		if i < len(lines) {
			fmt.Fprint(cv.termOut, line)
		}
		fmt.Fprint(cv.termOut, "\r\n")
	}
}

// DrawMenu 仅重绘菜单区（辅区域）：平铺网格 + 提示行，当前项反色高亮。
func (cv *Canvas) DrawMenu(items []string, cur int) {
	_, rows, itemWidth := menuGridSize(items, cv.width)
	cols := menuGridCols(cv.width, itemWidth, len(items))

	fmt.Fprintf(cv.termOut, "\x1b[%d;1H", cv.menuTop)
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
				fmt.Fprint(cv.termOut, color.New(color.ReverseVideo).Sprint(line))
			} else {
				fmt.Fprint(cv.termOut, line)
			}
			if c < cols-1 && i < len(items)-1 {
				fmt.Fprint(cv.termOut, " ")
			}
		}
		fmt.Fprint(cv.termOut, "\x1b[K\r\n")
	}
	fmt.Fprintf(cv.termOut, "\x1b[K请选择（←/→ 移动，回车确认，ESC 取消）: ")
}

// MenuTop 返回菜单区起始行（供 selectMenuKeyNav 定位重绘时使用）。
func (cv *Canvas) MenuTop() int { return cv.menuTop }

// ResetFeedback 清空反馈区内容缓冲并重绘（清空）。
func (cv *Canvas) ResetFeedback() {
	cv.outLines = nil
	cv.outCur = ""
	cv.renderFeedback()
}

// Write 实现 io.Writer：把字节写入反馈区缓冲并整区重绘（支持滚动）。
func (cv *Canvas) Write(p []byte) (int, error) {
	for _, r := range string(p) {
		switch r {
		case '\n':
			cv.outLines = append(cv.outLines, cv.outCur)
			cv.outCur = ""
			// 超出反馈区高度：丢弃最旧一行（滚动）
			for len(cv.outLines) > cv.outH {
				cv.outLines = cv.outLines[1:]
			}
		case '\r':
			// 忽略 CR（表格/日志可能输出 \r\n）
		default:
			cv.outCur += string(r)
			if displayWidth(cv.outCur) > cv.width {
				cv.outCur = truncateWidth(cv.outCur, cv.width)
			}
		}
	}
	cv.renderFeedback()
	return len(p), nil
}

// renderFeedback 将反馈区缓冲整区重绘（定位到反馈区顶部逐行输出）。
func (cv *Canvas) renderFeedback() {
	fmt.Fprintf(cv.termOut, "\x1b[%d;1H", cv.outTop)
	lines := append(append([]string{}, cv.outLines...), cv.outCur)
	for i := 0; i < cv.outH; i++ {
		var line string
		if i < len(lines) {
			line = lines[i]
		}
		fmt.Fprint(cv.termOut, "\x1b[K")
		if line != "" {
			fmt.Fprint(cv.termOut, line)
		}
		fmt.Fprint(cv.termOut, "\r\n")
	}
}

// BeginOutput 将 os.Stdout / color.Output 重定向到反馈区。
// 由于 os.Stdout 是 *os.File，使用 os.Pipe：写端作为临时 os.Stdout，
// 读端由 goroutine 持续读入反馈区缓冲（操作输出实时显示在反馈区并滚动）。
func (cv *Canvas) BeginOutput() {
	if cv.outputActive {
		return
	}
	r, w, err := os.Pipe()
	if err != nil {
		return // 重定向失败则保持原样输出（如非终端场景）
	}
	cv.savedStdout = os.Stdout
	cv.savedColor = color.Output
	cv.outPipeR, cv.outPipeW = r, w
	os.Stdout = w
	color.Output = cv
	cv.outputActive = true
	cv.outDone = make(chan struct{})
	go func() {
		defer close(cv.outDone)
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				cv.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
}

// EndOutput 恢复被重定向的输出并等待管道读取完成。
func (cv *Canvas) EndOutput() {
	if !cv.outputActive {
		return
	}
	os.Stdout = cv.savedStdout
	color.Output = cv.savedColor
	cv.outPipeW.Close() // 关闭写端 → 读端 EOF → goroutine 退出
	<-cv.outDone
	cv.outPipeR.Close()
	cv.outputActive = false
}

// RunAction 在反馈区执行一次操作：清空反馈区 → 标题与 os.Stdout/color.Output
// 输出重定向到反馈区 → 执行 fn → 恢复输出。操作输出（含错误）只出现在反馈区，
// 不影响主区域（证书列表）与菜单区。
func (cv *Canvas) RunAction(title string, fn func()) {
	cv.ResetFeedback()
	cv.Write([]byte("══════ " + title + " ══════\n"))

	cv.BeginOutput()
	defer cv.EndOutput()
	fn()
}

// truncateWidth 按显示宽度截断字符串到 max 列。
func truncateWidth(s string, max int) string {
	w := 0
	var out []rune
	for _, r := range s {
		rw := 1
		if displayWidth(string(r)) == 2 {
			rw = 2
		}
		if w+rw > max {
			break
		}
		w += rw
		out = append(out, r)
	}
	return string(out)
}
