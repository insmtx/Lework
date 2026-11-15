package main

import (
	"io"
	"os"
)

// isTerminal 判断输出是不是交互终端。
//
// 只有终端才允许打印二维码与短码等一次性凭据：重定向到文件、journal 或 CI
// 日志时静默跳过，等同于不把凭据写进日志。控制面启动不再打印任何凭据（见
// announceStartup），这个判定现在只服务节点 agent 的配对输出。
func isTerminal(out io.Writer) bool {
	file, ok := out.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
