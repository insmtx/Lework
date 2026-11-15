// Package pairing 生成手机端扫码配对用的深链。
//
// 链接里带着 enrollment token，属于机密信息：调用方只能把它写到启动时的
// 终端输出，绝不能经过 logger，也不得随日志落盘（见 controlplane/config
// 对令牌的约束）。这个包本身不打印、不记录，只负责拼装。
package pairing

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Params 描述配对链接的输入。
type Params struct {
	// PublicURL 是手机访问控制面的基址，例如 http://dev.ckeyer.com。
	PublicURL string
	// Token 是 enrollment token，随链接一起下发给客户端。
	Token string
	// ExpoURL 非空时生成 Expo Go 可识别的 exp:// 链接（开发期用），
	// 否则生成 lework:// 链接（安装版 APK）。
	ExpoURL string
}

// Link 返回配对深链，参数全部做 percent-encoding。
//
// 两种链接指向同一个 App 路由 /connect，区别只在 scheme：
//   - lework://connect?url=…&token=…       安装版 APK 直接接管
//   - exp://host:port/--/connect?url=…      Expo Go 先加载工程再进该路由
func Link(p Params) (string, error) {
	base, err := NormalizePublicURL(p.PublicURL)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(p.Token)
	if token == "" {
		return "", fmt.Errorf("pairing: token is required")
	}
	query := "url=" + url.QueryEscape(base) + "&token=" + url.QueryEscape(token)
	if strings.TrimSpace(p.ExpoURL) == "" {
		return "lework://connect?" + query, nil
	}
	prefix, err := expoPrefix(p.ExpoURL)
	if err != nil {
		return "", err
	}
	return prefix + "/--/connect?" + query, nil
}

// NormalizePublicURL 校验并收敛手机侧的控制面地址。
//
// 只接受 http(s)，去掉查询串、锚点与末尾斜杠：链接里的 url 会被 App 直接
// 当作 baseUrl 拼 /api，多一个斜杠就会拼出 //api。
func NormalizePublicURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("pairing: public URL is required")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("pairing: public URL %q is invalid: %w", raw, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("pairing: public URL %q must start with http:// or https://", raw)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("pairing: public URL %q has no host", raw)
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

// ResolvePublicURL 决定写进二维码的控制面地址。
//
// 明确给了 publicURL 就用它；否则用本机局域网 IPv4 + 监听端口，让同一个
// 局域网里的手机能直接扫。监听回环且没有局域网地址时返回错误，由调用方
// 提示用户补 --public-url。
func ResolvePublicURL(listenAddr, publicURL string) (string, error) {
	if strings.TrimSpace(publicURL) != "" {
		return NormalizePublicURL(publicURL)
	}
	_, port, err := net.SplitHostPort(strings.TrimSpace(listenAddr))
	if err != nil {
		return "", fmt.Errorf("pairing: listen address %q is invalid: %w", listenAddr, err)
	}
	ip := lanIPv4()
	if ip == "" {
		return "", fmt.Errorf("pairing: no LAN IPv4 found; pass --public-url so the phone knows where to connect")
	}
	return "http://" + net.JoinHostPort(ip, port), nil
}

// expoPrefix 把 http(s)://host:port 换成 exp(s)://host:port。
//
// Expo Go 只认 exp:// 与 exps://，路径与查询串由调用方拼接，这里只取源点，
// 避免把用户误填的子路径带进深链。
func expoPrefix(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("pairing: expo URL %q is invalid: %w", raw, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("pairing: expo URL %q must start with http:// or https://", raw)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("pairing: expo URL %q has no host", raw)
	}
	scheme := "exp"
	if parsed.Scheme == "https" {
		scheme = "exps"
	}
	return scheme + "://" + parsed.Host, nil
}

// lanIPv4 返回第一个非回环、非链路本地的 IPv4 地址。
//
// 只用于没指定 --public-url 时的兜底：本机常常同时有容器网桥和真实网卡，
// 取第一个可用地址即可，扫不通时用户会显式指定域名。
func lanIPv4() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipNet.IP.To4()
		if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			continue
		}
		return ip.String()
	}
	return ""
}
