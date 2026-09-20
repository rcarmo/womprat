package edge

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	maxDownloadCookies       = 300
	maxDownloadCookieBytes   = 64 * 1024
	maxPendingCookieRequests = 64
)

type cookieRequest struct {
	chromium *Chromium
	callback func([]*http.Cookie, error)
	handler  *ICoreWebView2GetCookiesCompletedHandler
}

func (r *cookieRequest) QueryInterface(_, _ uintptr) uintptr { return 0 }
func (r *cookieRequest) AddRef() uintptr                     { return 1 }
func (r *cookieRequest) Release() uintptr                    { return 1 }

func (r *cookieRequest) GetCookiesCompleted(result uintptr, list *ICoreWebView2CookieList) uintptr {
	cookies, err := cookieListToHTTP(result, list)
	r.chromium.cookieMu.Lock()
	delete(r.chromium.cookieRequests, r.handler)
	r.chromium.cookieMu.Unlock()
	r.callback(cookies, err)
	return 0
}

func cookieListToHTTP(result uintptr, list *ICoreWebView2CookieList) ([]*http.Cookie, error) {
	if int32(result) < 0 || list == nil {
		return nil, fmt.Errorf("GetCookies callback: HRESULT %#x", result)
	}
	defer list.Release()
	count, err := list.Count()
	if err != nil {
		return nil, err
	}
	if count > maxDownloadCookies {
		return nil, fmt.Errorf("too many cookies for managed download: %d", count)
	}
	cookies := make([]*http.Cookie, 0, count)
	totalBytes := 0
	for index := uint32(0); index < count; index++ {
		cookie, err := list.Item(index)
		if err != nil {
			return nil, err
		}
		name, nameErr := cookie.Name()
		value, valueErr := cookie.Value()
		domain, domainErr := cookie.Domain()
		path, pathErr := cookie.Path()
		httpOnly, httpOnlyErr := cookie.IsHTTPOnly()
		secure, secureErr := cookie.IsSecure()
		expires, expiresErr := cookie.Expires()
		cookie.Release()
		for _, err := range []error{nameErr, valueErr, domainErr, pathErr, httpOnlyErr, secureErr, expiresErr} {
			if err != nil {
				return nil, err
			}
		}
		if !validCookiePair(name, value) {
			continue
		}
		totalBytes += len(name) + len(value) + 2
		if totalBytes > maxDownloadCookieBytes {
			return nil, fmt.Errorf("cookies for managed download exceed %d bytes", maxDownloadCookieBytes)
		}
		converted := &http.Cookie{Name: name, Value: value, Domain: domain, Path: path, HttpOnly: httpOnly, Secure: secure}
		if expires > 0 {
			converted.Expires = time.Unix(int64(expires), 0)
		}
		cookies = append(cookies, converted)
	}
	return cookies, nil
}

func validCookiePair(name, value string) bool {
	return name != "" && !strings.ContainsAny(name, "\x00\r\n;= \t") && !strings.ContainsAny(value, "\x00\r\n;")
}

func (e *Chromium) GetCookies(uri string, callback func([]*http.Cookie, error)) error {
	if callback == nil {
		return fmt.Errorf("nil cookie callback")
	}
	manager, err := e.webview.GetCookieManager()
	if err != nil {
		return err
	}
	defer manager.Release()
	request := &cookieRequest{chromium: e, callback: callback}
	request.handler = newICoreWebView2GetCookiesCompletedHandler(request)
	e.cookieMu.Lock()
	if len(e.cookieRequests) >= maxPendingCookieRequests {
		e.cookieMu.Unlock()
		return fmt.Errorf("too many pending cookie requests")
	}
	e.cookieRequests[request.handler] = request
	e.cookieMu.Unlock()
	if err := manager.GetCookies(uri, request.handler); err != nil {
		e.cookieMu.Lock()
		delete(e.cookieRequests, request.handler)
		e.cookieMu.Unlock()
		return err
	}
	return nil
}
