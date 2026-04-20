package inspect

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

// DevTarget is the hardcoded Vite dev server address. The --dev flag
// enables a reverse proxy to this address for non-API requests.
const DevTarget = "http://127.0.0.1:5173"

func newDevProxy(target string) (http.Handler, error) {
	parsed, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	proxy := httputil.NewSingleHostReverseProxy(parsed)
	return proxy, nil
}
