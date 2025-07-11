package main

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sonacy/go-whistle-lite/internal/logx"
	"github.com/sonacy/go-whistle-lite/proxy"
	"github.com/sonacy/go-whistle-lite/rules"
	"github.com/sonacy/go-whistle-lite/sysproxy"
)

/* ---------- flags ---------- */
var port = flag.Int("port", 8899, "listening port")

func main() {
	flag.Parse()
	addr := fmt.Sprintf(":%d", *port)

	/* ---- ① 绑定端口，若占用则尝试强制释放 ---- */
	ln, err := tryListen(addr, *port)
	if err != nil {
		log.Fatalf("[gw-lite] cannot listen on %s : %v", addr, err)
	}
	defer ln.Close()

	/* ---- ② macOS 全局代理 ---- */
	cleanupProxy := func() {}
	if runtime.GOOS == "darwin" {
		if err := sysproxy.Enable("127.0.0.1", *port); err != nil {
			log.Fatalf("enable proxy: %v", err)
		}
		cleanupProxy = sysproxy.Disable
	}
	defer cleanupProxy()

	/* ---- ③ 创建服务器 ---- */
	srv := &http.Server{
		Handler:           http.HandlerFunc(proxy.HandleRequest),
		ReadHeaderTimeout: 5 * time.Second,
	}
	srv.SetKeepAlivesEnabled(false) // 避免 TIME_WAIT 占端口

	/* ---- ④ 捕获信号优雅退出 ---- */
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)

	go func() {
		for sig := range quit {
			if sig == syscall.SIGHUP {
				// 手动触发热加载
				log.Println("[gw-lite] SIGHUP received → reload rules")
				rules.ForceReload() // ↓ 新增 helper
				continue
			}
			log.Println("[gw-lite] shutting down …")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = srv.Shutdown(ctx)
			cancel()
		}
	}()

	/* ---- ⑤ Serve ---- */
	logx.D("[gw-lite] listening on %s", addr)
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		logx.D("[gw-lite] serve error: %v", err)
	}
	log.Println("[gw-lite] stopped")
}

/* ---------- helper: tryListen + forceFreePort ---------- */

func tryListen(addr string, port int) (net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err == nil {
		return ln, nil
	}
	// 仅处理“address already in use”
	if !strings.Contains(err.Error(), "address already in use") {
		return nil, err
	}
	// 非 Unix 系统直接返回
	if runtime.GOOS == "windows" {
		return nil, err
	}

	logx.D("[gw-lite] port %d busy, trying to free …", port)
	if ferr := forceFreePort(port); ferr != nil {
		return nil, fmt.Errorf("%v (port busy, free failed: %v)", err, ferr)
	}
	time.Sleep(500 * time.Millisecond) // 等内核真正释放

	return net.Listen("tcp", addr)
}

func forceFreePort(p int) error {
	cmd := exec.Command("lsof", "-ti", fmt.Sprintf("tcp:%d", p))
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("lsof: %v", err)
	}
	sc := bufio.NewScanner(bytes.NewReader(out))
	var pids []int
	for sc.Scan() {
		if id, e := strconv.Atoi(strings.TrimSpace(sc.Text())); e == nil {
			pids = append(pids, id)
		}
	}
	if len(pids) == 0 {
		return fmt.Errorf("no process found")
	}
	for _, pid := range pids {
		logx.D("[gw-lite] kill -9 %d", pid)
		if kerr := syscall.Kill(pid, syscall.SIGKILL); kerr != nil {
			logx.D("kill %d: %v", pid, kerr)
		}
	}
	return nil
}
