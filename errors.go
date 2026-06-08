package main

import (
	"fmt"
	"net/http"
)

const errorPageTemplate = `<!DOCTYPE html>
<html><head><meta charset="UTF-8"><meta name="color-scheme" content="dark">
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{background:#0d1117;color:#e6edf3;font-family:system-ui,sans-serif;
  display:flex;align-items:center;justify-content:center;min-height:100vh;padding:2rem}
.err{text-align:center;max-width:420px}
.code{font-size:3rem;font-weight:800;color:#58a6ff;margin-bottom:.5rem}
.msg{font-size:.95rem;color:#8b949e;line-height:1.6}
.detail{margin-top:1rem;padding:.8rem 1rem;background:#161b22;border:1px solid #30363d;
  border-radius:8px;font-family:monospace;font-size:.8rem;color:#f85149;word-break:break-all}
</style></head><body>
<div class="err">
  <div class="code">%d</div>
  <div class="msg">%s</div>
  %s
</div></body></html>`

func httpError(w http.ResponseWriter, code int, msg string, detail string) {
	detailHTML := ""
	if detail != "" {
		detailHTML = fmt.Sprintf(`<div class="detail">%s</div>`, detail)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	fmt.Fprintf(w, errorPageTemplate, code, msg, detailHTML)
}
