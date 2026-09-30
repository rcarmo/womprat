package edge

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	maxDownloadCookies       = 300
	maxDownloadCookieBytes   = 64 * 1024
	maxPendingCookieRequests = 64
)

type cookieRequest struct {
	callback  func([]*http.Cookie, error)
	handler   *ICoreWebView2GetCookiesCompletedHandler
	completed sync.Once
}

func (r *cookieRequest) GetCookiesCompleted(result uintptr, list *ICoreWebView2CookieList) uintptr {
	r.completed.Do(func() {
		cookies, err := cookieListToHTTP(result, list)
		callback := r.callback
		r.callback = nil
		callback(cookies, err)
	})
	return 0
}

func cookieListToHTTP(result uintptr, list *ICoreWebView2CookieList) ([]*http.Cookie, error) {
	if int32(result) < 0 || list == nil {
		return nil, fmt.Errorf("GetCookies callback: HRESULT %#x", result)
	}
	// Invoke supplies a borrowed [in] pointer. WebView2 owns this list and
	// releases it after the callback; only GetValueAtIndex outputs are owned.
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
	if e == nil || e.webview == nil {
		return fmt.Errorf("browser is not available for cookie retrieval")
	}
	manager, err := e.webview.GetCookieManager()
	if err != nil {
		return err
	}
	defer manager.Release()
	request := &cookieRequest{callback: callback}
	request.handler = newICoreWebView2GetCookiesCompletedHandler(request)
	// Keep our creation reference across the call (which may invoke inline).
	// An asynchronous WebView2 operation must AddRef before returning.
	defer getCookiesRelease(request.handler)
	request.handler.onRelease = func() {
		e.cookieMu.Lock()
		delete(e.cookieRequests, request.handler)
		e.cookieMu.Unlock()
	}
	e.cookieMu.Lock()
	if len(e.cookieRequests) >= maxPendingCookieRequests {
		e.cookieMu.Unlock()
		return fmt.Errorf("too many pending cookie requests")
	}
	if e.cookieRequests == nil {
		e.cookieRequests = make(map[*ICoreWebView2GetCookiesCompletedHandler]*cookieRequest)
	}
	e.cookieRequests[request.handler] = request
	e.cookieMu.Unlock()
	return manager.GetCookies(uri, request.handler)
}
