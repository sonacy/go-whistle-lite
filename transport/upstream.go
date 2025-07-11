package transport

import (
	"crypto/tls"
	"net/http"
	"time"

	http2 "golang.org/x/net/http2"
)

var Upstream *http.Transport

func init() {
	Upstream = &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		MaxIdleConns:        2000,
		MaxIdleConnsPerHost: 200,
		IdleConnTimeout:     90 * time.Second,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
	}
	_ = http2.ConfigureTransport(Upstream) // enable h2 pooling
}
