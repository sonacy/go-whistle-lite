package rules

import (
	"bufio"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sonacy/go-whistle-lite/internal/logx"
)

/* ---------- action constants ---------- */

const (
	ActMapRemote  = "mapRemote"
	ActMapLocal   = "mapLocal"
	ActStatus     = "status"
	ActReqHeader  = "reqHeader"
	ActRespHeader = "respHeader"
)

/* ---------- matcher implementations ---------- */

type matcher interface{ Match(string) bool }

/* 精确匹配 */
type exact string

func (e exact) Match(s string) bool { return s == string(e) }

/* 通配符 (* ? 不跨目录) */
type wildcard string

func (w wildcard) Match(s string) bool {
	ok, _ := filepath.Match(string(w), s)
	return ok
}

/* 前缀匹配：pattern 以 '*' 结尾且只这一处 '*' */
type prefix string

func (p prefix) Match(s string) bool { return strings.HasPrefix(s, string(p)) }

/* 正则匹配 rx:// */
type regex struct{ *regexp.Regexp }

func (r regex) Match(s string) bool { return r.Regexp.MatchString(s) }

/* ---------- Rule ---------- */

type Rule struct {
	Host    matcher
	Path    matcher
	PathRaw string // 原始 Path 文本：判断是否 * 结尾
	Action  string
	Param   string
}

/* ---------- hot-reload cache ---------- */

var (
	txtFile  = "rules.txt"
	jsonFile = "rules/rules.json"

	mu   sync.RWMutex
	list []*Rule
	mt   time.Time
)

/* ---------- API: Match ---------- */

func Match(u *url.URL) *Rule {
	load()
	mu.RLock()
	defer mu.RUnlock()
	for _, r := range list {
		if r.Host != nil && !r.Host.Match(u.Host) {
			continue
		}
		if r.Path != nil && !r.Path.Match(u.Path) {
			continue
		}
		return r
	}
	return nil
}

/* ---------- internal loader ---------- */

func load() {
	fi, err := os.Stat(txtFile)
	if err != nil {
		if mt.IsZero() {
			loadLegacy()
		}
		return
	}
	if fi.ModTime() == mt {
		return
	} // no change

	rs, err := parseDSL(txtFile)
	if err != nil {
		logx.D("[rules] parse error: %v", err)
		return
	}

	mu.Lock()
	list, mt = rs, fi.ModTime()
	mu.Unlock()
	logx.D("[rules] %d rule(s) loaded", len(rs))
}

func parseDSL(p string) ([]*Rule, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []*Rule
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Fields(line) // split by space / tab
		if len(parts) < 2 {
			continue
		}

		host, path := splitHostPath(parts[0])
		action, param := splitProto(parts[1])

		out = append(out, &Rule{
			Host:    compileMatcher(host),
			Path:    compileMatcher(path),
			PathRaw: path,
			Action:  action,
			Param:   param,
		})
	}
	return out, sc.Err()
}

/* ---------- matcher helpers ---------- */

func splitHostPath(s string) (host, path string) {
	if strings.HasPrefix(s, "/") {
		return "", s
	}
	if i := strings.IndexByte(s, '/'); i >= 0 {
		return s[:i], s[i:]
	}
	return s, ""
}

func splitProto(s string) (act, param string) {
	if i := strings.Index(s, "://"); i >= 0 {
		return s[:i], s[i+3:]
	}
	return s, ""
}

func compileMatcher(p string) matcher {
	if p == "" {
		return nil
	}
	if strings.HasPrefix(p, "rx://") {
		return regex{regexp.MustCompile(strings.TrimPrefix(p, "rx://"))}
	}
	/* NEW —— 末尾带单个 '*'  → 前缀匹配 */
	if strings.HasSuffix(p, "*") && strings.Count(p, "*") == 1 {
		return prefix(strings.TrimSuffix(p, "*"))
	}
	if strings.ContainsAny(p, "*?") {
		return wildcard(p)
	}
	return exact(p)
}

/* ---------- legacy JSON fallback ---------- */

func loadLegacy() {
	data, err := os.ReadFile(jsonFile)
	if err != nil {
		return
	}
	var raw []struct{ Match, Action, Target string }
	if json.Unmarshal(data, &raw) != nil {
		return
	}

	var rs []*Rule
	for _, r := range raw {
		h, p := splitHostPath(r.Match)
		rs = append(rs, &Rule{
			Host:    compileMatcher(h),
			Path:    compileMatcher(p),
			PathRaw: p,
			Action:  r.Action,
			Param:   r.Target,
		})
	}
	mu.Lock()
	list = rs
	mu.Unlock()
	logx.D("[rules] %d legacy JSON rule(s) loaded", len(rs))
}

/* ---------- helpers for status / header rules ---------- */

func ParseStatus(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	return n, err == nil
}

func ParseHeaderParam(p string) (op, key, val string) {
	i := strings.IndexByte(p, ':')
	if i < 0 {
		return "", p, ""
	}
	op = p[:i]
	rest := p[i+1:]
	if j := strings.IndexByte(rest, '='); j >= 0 {
		return op, rest[:j], rest[j+1:]
	}
	return op, rest, ""
}

// ForceReload 供 SIGHUP 调用
func ForceReload() {
	mu.Lock()
	mt = time.Time{} // 置空触发下次 load()
	mu.Unlock()
}
