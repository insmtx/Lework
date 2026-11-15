package main

import (
	"fmt"
	"io"
	"os"

	qrterminal "github.com/mdp/qrterminal/v3"
	"github.com/spf13/cobra"
	"github.com/ygpkg/yg-go/logs"

	controlplaneconfig "github.com/insmtx/lework/backend/internal/controlplane/config"
	"github.com/insmtx/lework/backend/internal/controlplane/pairing"
)

// pairOptions 控制启动时是否打印手机配对二维码。
type pairOptions struct {
	// publicURL 是手机访问控制面的地址；空值时退回本机局域网 IP。
	publicURL string
	// expoURL 非空时二维码改用 exp:// 链接，供 Expo Go 调试期直接扫。
	expoURL string
	// noQR 关闭二维码输出，只保留普通启动日志。
	noQR bool
}

// addPairingFlags 把三个配对相关 flag 挂到命令上。
func addPairingFlags(cmd *cobra.Command, opts *pairOptions) {
	flags := cmd.Flags()
	flags.StringVar(&opts.publicURL, "public-url", "",
		"mobile-facing control plane URL, e.g. http://dev.ckeyer.com (default: LAN IP + listen port)")
	flags.StringVar(&opts.expoURL, "expo-url", "",
		"Metro dev server URL, e.g. http://host:8082; makes the pairing QR an Expo Go link")
	flags.BoolVar(&opts.noQR, "no-qr", false, "do not print the mobile pairing QR code")
}

// announcePairing 打印手机配对二维码与等价链接。
//
// 链接里带着 enrollment token，只在 stdout 是终端时输出：重定向到文件、
// journal 或 CI 日志时静默跳过，否则等同于把令牌写进日志。
// 任何失败都只降级为警告，不能影响控制面启动。
func announcePairing(out io.Writer, cfg controlplaneconfig.Config, opts pairOptions) {
	if opts.noQR || !isTerminal(out) {
		return
	}
	base, err := pairing.ResolvePublicURL(cfg.ListenAddr, opts.publicURL)
	if err != nil {
		logs.Warnf("skip pairing QR: %v", err)
		return
	}
	link, err := pairing.Link(pairing.Params{
		PublicURL: base,
		Token:     cfg.EnrollmentToken,
		ExpoURL:   opts.expoURL,
	})
	if err != nil {
		logs.Warnf("skip pairing QR: %v", err)
		return
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Pair the mobile app (scan with the camera, Expo Go, or the app):")
	qrterminal.GenerateWithConfig(link, qrterminal.Config{
		Level:      qrterminal.L,
		Writer:     out,
		HalfBlocks: true,
	})
	fmt.Fprintln(out, link)
	fmt.Fprintf(out, "If scanning fails, fill in the app settings: server %s, token %s\n", base, cfg.EnrollmentToken)
	fmt.Fprintln(out)
}

// isTerminal 判断输出是不是交互终端；只有终端才允许露出令牌。
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
