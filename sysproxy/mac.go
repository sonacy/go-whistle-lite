//go:build darwin

package sysproxy

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/sonacy/go-whistle-lite/internal/logx"
)

// Enable 开启系统 HTTP/HTTPS 代理
func Enable(host string, port int) error {
	svcs, err := list()
	if err != nil {
		return err
	}
	for _, s := range svcs {
		logx.D("[proxyctl] enable %s %s:%d", s, host, port)
		for _, args := range [][]string{
			{"-setwebproxy", s, host, fmt.Sprint(port)},
			{"-setsecurewebproxy", s, host, fmt.Sprint(port)},
			{"-setwebproxystate", s, "on"},
			{"-setsecurewebproxystate", s, "on"},
		} {
			if out, err := exec.Command("networksetup", args...).CombinedOutput(); err != nil {
				return fmt.Errorf("%v: %s", err, out)
			}
		}
	}
	return nil
}

// Disable 关闭系统代理
func Disable() {
	svcs, _ := list()
	for _, s := range svcs {
		logx.D("[proxyctl] disable %s", s)
		exec.Command("networksetup", "-setwebproxystate", s, "off").Run()
		exec.Command("networksetup", "-setsecurewebproxystate", s, "off").Run()
	}
}

func list() ([]string, error) {
	out, err := exec.Command("networksetup", "-listallnetworkservices").Output()
	if err != nil {
		return nil, err
	}
	var svcs []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		t := strings.TrimSpace(sc.Text())
		if t == "" || strings.HasPrefix(t, "An asterisk") {
			continue
		}
		svcs = append(svcs, strings.TrimPrefix(t, "* "))
	}
	return svcs, nil
}
