// mitm/mitm.go — downstream 支持 H1 + H2
package mitm

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	http2 "golang.org/x/net/http2"

	"github.com/sonacy/go-whistle-lite/rules"
)

/* ------------ CONNECT entry ------------ */

func Intercept(w http.ResponseWriter, r *http.Request) {
	/* 1. hijack client socket */
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack unsupported", http.StatusInternalServerError)
		return
	}
	cliRaw, _, _ := hj.Hijack()
	_, _ = cliRaw.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	/* 2. gen fake cert & TLS with client */
	host := extractHost(r.Host)
	pair, err := getHostCert(host)
	if err != nil {
		log.Printf("cert: %v", err)
		cliRaw.Close()
		return
	}
	cert, _ := tls.X509KeyPair(pair.CertPEM, pair.KeyPEM)

	cli := tls.Server(cliRaw, &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h2", "http/1.1"},
	})
	if err := cli.Handshake(); err != nil {
		log.Printf("TLS handshake: %v", err)
		cli.Close()
		return
	}

	np := cli.ConnectionState().NegotiatedProtocol
	if np == "h2" {
		serveH2(cli)
		return
	}

	/* 3. HTTP/1.x path */
	up, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", r.Host, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		log.Printf("dial up: %v", err)
		cli.Close()
		return
	}
	pipeHTTP1(cli, up)
}

/* ------------ HTTP/2 downstream ------------ */

func serveH2(conn net.Conn) {
	h2s := &http2.Server{}
	// ServeConn 阻塞直到连接结束；无需捕获返回值
	h2s.ServeConn(conn, &http2.ServeConnOpts{
		BaseConfig: &http.Server{
			Handler: http.HandlerFunc(h2Handler),
		},
	})
}

func h2Handler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Scheme == "" { // 已加的 Scheme 补丁
		r.URL.Scheme = "https"
	}
	if r.URL.Host == "" { // ← 新增：补上 Host
		r.URL.Host = r.Host
	}

	orig := r.URL
	dst := buildMapRemoteURL(rules.Match(orig), orig)

	/* request-side rules */
	if ru := rules.Match(orig); ru != nil {
		switch ru.Action {
		case rules.ActStatus:
			if code, ok := rules.ParseStatus(ru.Param); ok {
				w.WriteHeader(code)
				return
			}
		case rules.ActMapLocal:
			serveLocalHTTP(w, r, ru.Param)
			return
		case rules.ActReqHeader:
			applyHeader(&r.Header, ru.Param)
		}
	}

	out, _ := http.NewRequest(r.Method, dst.String(), r.Body)
	out.Header = r.Header.Clone()

	resp, err := http.DefaultTransport.RoundTrip(out)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, err.Error())
		return
	}
	defer resp.Body.Close()

	if ru := rules.Match(orig); ru != nil && ru.Action == rules.ActRespHeader {
		applyHeader(&resp.Header, ru.Param)
	}

	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

/* ------------ HTTP/1.x downstream ------------ */

func pipeHTTP1(cli, up net.Conn) {
	defer cli.Close()
	defer up.Close()
	rd := bufio.NewReader(cli)
	for {
		req, err := http.ReadRequest(rd)
		if err != nil {
			if err != io.EOF {
				log.Printf("read: %v", err)
			}
			return
		}

		orig := &url.URL{Scheme: "https", Host: req.Host, Path: req.URL.Path, RawQuery: req.URL.RawQuery}
		dst := buildMapRemoteURL(rules.Match(orig), orig)

		if ru := rules.Match(orig); ru != nil {
			switch ru.Action {
			case rules.ActStatus:
				if code, ok := rules.ParseStatus(ru.Param); ok {
					fmt.Fprintf(cli, "HTTP/1.1 %d \r\nContent-Length:0\r\n\r\n", code)
					continue
				}
			case rules.ActMapLocal:
				serveLocalTLS(cli, ru.Param)
				continue
			case rules.ActReqHeader:
				applyHeader(&req.Header, ru.Param)
			}
		}

		out, _ := http.NewRequest(req.Method, dst.String(), req.Body)
		out.Header = req.Header.Clone()

		resp, err := http.DefaultTransport.RoundTrip(out)
		if err != nil {
			log.Printf("rt: %v", err)
			return
		}

		if ru := rules.Match(orig); ru != nil && ru.Action == rules.ActRespHeader {
			applyHeader(&resp.Header, ru.Param)
		}
		resp.Write(cli)
		resp.Body.Close()
	}
}

/* ------------ shared helpers ------------ */

func buildMapRemoteURL(ru *rules.Rule, src *url.URL) *url.URL {
	if ru == nil || ru.Action != rules.ActMapRemote {
		return src
	}
	newURL := ru.Param
	if strings.HasSuffix(ru.PathRaw, "*") {
		prefix := strings.TrimSuffix(ru.PathRaw, "*")
		suffix := strings.TrimPrefix(src.Path, prefix)
		if !strings.HasSuffix(newURL, "/") && !strings.HasPrefix(suffix, "/") {
			newURL += "/"
		}
		newURL += suffix
	}
	u, _ := url.Parse(newURL)
	return u
}

func serveLocalTLS(c net.Conn, p string) {
	if strings.HasPrefix(p, "@") {
		b, _ := os.ReadFile(p[1:])
		fmt.Fprintf(c, "HTTP/1.1 200 OK\r\nContent-Length:%d\r\n\r\n", len(b))
		c.Write(b)
		return
	}
	fmt.Fprintf(c, "HTTP/1.1 200 OK\r\nContent-Length:%d\r\n\r\n%s", len(p), p)
}

func serveLocalHTTP(w http.ResponseWriter, r *http.Request, p string) {
	if strings.HasPrefix(p, "@") {
		http.ServeFile(w, r, p[1:])
		return
	}
	w.Write([]byte(p))
}

func applyHeader(h *http.Header, p string) {
	op, k, v := rules.ParseHeaderParam(p)
	switch strings.ToLower(op) {
	case "add":
		h.Add(k, v)
	case "set":
		h.Set(k, v)
	case "del", "remove":
		h.Del(k)
	}
}

func extractHost(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return hostport
}
