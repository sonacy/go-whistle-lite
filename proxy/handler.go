package proxy

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/sonacy/go-whistle-lite/internal/logx"
	"github.com/sonacy/go-whistle-lite/mitm"
	"github.com/sonacy/go-whistle-lite/rules"
	"github.com/sonacy/go-whistle-lite/transport"
)

var bufPool = sync.Pool{
	New: func() any { return make([]byte, 32<<10) }, // 32 KB
}

func copyStream(dst io.Writer, src io.Reader) (int64, error) {
	buf := bufPool.Get().([]byte)
	n, err := io.CopyBuffer(dst, src, buf)
	bufPool.Put(buf)
	return n, err
}

func HandleRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		logx.D("[CONNECT] %s", r.Host)
		mitm.Intercept(w, r)
		return
	}
	logx.D("[HTTP   ] %s %s", r.Method, r.URL.String())
	handleHTTP(w, r)
}

func handleHTTP(w http.ResponseWriter, r *http.Request) {
	target := r.URL
	if ru := rules.Match(r.URL); ru != nil {
		switch ru.Action {

		case rules.ActMapRemote:
			target = buildMapRemoteURL(ru, r.URL)

		case rules.ActStatus:
			if code, ok := rules.ParseStatus(ru.Param); ok {
				w.WriteHeader(code)
				return
			}

		case rules.ActMapLocal:
			serveLocal(w, r, ru.Param)
			return

		case rules.ActReqHeader:
			applyHeader(&r.Header, ru.Param)
		}
	}

	req, _ := http.NewRequest(r.Method, target.String(), r.Body)
	req.Header = r.Header.Clone()

	resp, err := transport.Upstream.RoundTrip(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if ru := rules.Match(r.URL); ru != nil && ru.Action == rules.ActRespHeader {
		applyHeader(&resp.Header, ru.Param)
	}

	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	copyStream(w, resp.Body)

	logx.D("[resp   ] %s %d", target, resp.StatusCode)
}

/* ---------- helpers ---------- */

func buildMapRemoteURL(rule *rules.Rule, src *url.URL) *url.URL {
	newURL := rule.Param

	// PathRaw 以 '*' 结尾 ⇒ 拼接后缀
	if strings.HasSuffix(rule.PathRaw, "*") {
		prefix := strings.TrimSuffix(rule.PathRaw, "*")
		suffix := strings.TrimPrefix(src.Path, prefix)

		if !strings.HasSuffix(newURL, "/") && !strings.HasPrefix(suffix, "/") {
			newURL += "/"
		}
		newURL += suffix
	}
	u, _ := url.Parse(newURL)
	return u
}

func serveLocal(w http.ResponseWriter, r *http.Request, p string) {
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
