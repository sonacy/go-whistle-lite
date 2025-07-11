# go‑whistle‑lite

A minimal, high‑performance **HTTP / HTTPS debugging proxy** written in Go 1.23.
Inspired by \[avwo/whistle] but re‑implemented from scratch:

* ⭐ **MapRemote / MapLocal**                – redirect requests or serve local files
* ⭐ **Wildcard / Prefix / RegExp** rules    – `*.cdn.com/*.js`, `path*`, `rx://...`
* ⭐ **Header rewrite**                      – add / set / delete (req & resp)
* ⭐ **Mock status**                         – `status://403`
* ⭐ **TLS MITM** with auto‑generated Root CA (5 years)
* ⭐ **HTTP/2 → proxy** **and** proxy → upstream (optional)
* ⭐ **Hot reload** – save `rules.txt` or `kill ‑HUP` to reload instantly
* ⭐ **macOS global‑proxy on/off** with sudo; Windows/Linux: manual/CLI flag

---

## Quick Start (macOS example)

```bash
# 1. clone & build (Go 1.23)
git clone https://github.com/sonacy/go-whistle-lite.git
cd go-whistle-lite
go mod tidy

# 2. run ( need sudo only for setting system proxy )
sudo go run main.go -port 8899
```

```
[proxyctl] enable Wi‑Fi 127.0.0.1:8899
[gw-lite] listening on :8899
```

1. First run generates `~/go-whistle-lite/rootCA.pem`
2. **Import & Always Trust** this root cert in Keychain / Windows cert store
3. Browser/system proxy ⇒ `127.0.0.1:8899` (HTTP + HTTPS)
4. Edit **rules/rules.txt** – save – immediately takes effect ✨

Stop with **Ctrl‑C** → server shuts down → proxy off.

---

## Rule DSL

| Pattern example          | Type       | Action protocol     | Param example                              |
| ------------------------ | ---------- | ------------------- | ------------------------------------------ |
| `www.google.com/`        | exact      | `mapRemote://`      | `http://localhost:8080`                    |
| `www.google.com/static*` | prefix `*` |                     | (suffix auto‑append)                       |
| `*.bytecdn.com/*.js`     | wildcard   | `mapLocal://`       | `@static/override.js` *(leading @ = file)* |
| `/api/v1/login`          | path only  | `status://`         | `403`                                      |
| `rx://^/api/.*\.json$`   | regexp     | `respHeader://Del:` | `Cache-Control`                            |
| *any*                    |  –         | `reqHeader://Add:`  | `X-Demo=1`                                 |

### Header syntax

```
reqHeader://Add:Key=Value
reqHeader://Set:Key=Value
respHeader://Del:Key
```

---

## Hot Reload

* **Auto** – edit & save `rules.txt` (fsnotify watcher)
* **Manual** – `kill -HUP $(pgrep gw-lite)`  ➜ logs show reload

---

## HTTP/2 support *(optional)*

HTTP/2 to upstream is automatic.
To enable **browser→proxy** H2, `mitm/mitm.go` contains an `http2` path using `golang.org/x/net/http2`. Already integrated – just `go mod tidy`.

---

## CLI flags

```bash
-port           # listening port (default 8899)
```

macOS proxy helper auto‑applies the chosen port.

---

## Troubleshooting

| Symptom                             | Fix                                                                                 |
| ----------------------------------- | ----------------------------------------------------------------------------------- |
| Browser shows NET::ERR\_CERT\_AUTH… | Import rootCA, set **Always Trust**.                                                |
| `listen tcp :8899: address in use`  | `sudo pkill -f go-whistle-lite` or `-port` swap.                                    |
| `unsupported protocol scheme ""`    | ensure `rules.PathRaw` ends with `*` **and** `@https` scheme fix (already in code). |
| `no Host in request URL`            | make sure HTTP/2 patch imported; latest `mitm.go` sets `r.URL.Host = r.Host`.       |

---

## Roadmap

* 🔧   Delay / Throttle / Replace body rules
* 🖥️   Minimal Web UI for rule editing & traffic view
* 🔧   Windows CLI for auto proxy on/off
* 🐳   Docker image & Kubernetes side‑car

PRs welcome!  Enjoy debugging ✨
