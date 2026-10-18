package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// The debugger's page can exist before its initial navigation can fetch the
// local app. Bootstrap alone waits for the existing full browser assertion.
// Restored markers and every later read/set assertion stay unchanged.
func criuEpochBrowserBootstrap(id string) (output string, resultErr error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	run := func(arguments ...string) (string, error) {
		command := exec.CommandContext(ctx, criuRuntime,
			append([]string{"--root", criuRoot + "/runtime", "exec", id}, arguments...)...)
		command.WaitDelay = 2 * time.Second
		data, err := command.CombinedOutput()
		return string(data), err
	}
	var attempts []map[string]any
	defer func() {
		record("sysbox-browser-bootstrap-ready", start, asJSON(map[string]any{
			"attempts": attempts,
			"scope":    "initial source preparation only; repeat existing browser-state set assertion until actual app fetch and saved RAM marker succeed, bounded 20s; no restored/readiness assertion changed",
		}), resultErr)
	}()
	for {
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("browser initial preparation deadline: %w", err)
		}
		attempt := time.Now()
		out, err := run("node", "/opt/browser-state.mjs", "set")
		if err == nil && strings.TrimSpace(out) != "" {
			attempts = append(attempts, map[string]any{"elapsed_ms": time.Since(attempt).Milliseconds(), "ready": true})
			return out, nil
		}
		if err == nil {
			err = fmt.Errorf("browser returned an empty initial RAM marker")
		}
		if len(out) > 1024 {
			out = out[len(out)-1024:]
		}
		pages, pagesErr := run("node", "-e",
			"fetch('http://127.0.0.1:9222/json/list').then(r=>r.json()).then(rows=>console.log(JSON.stringify(rows.map(({type,url,title})=>({type,url,title}))))).catch(e=>{console.error(String(e));process.exitCode=1})")
		if len(pages) > 2048 {
			pages = pages[:2048]
		}
		attempts = append(attempts, map[string]any{
			"elapsed_ms": time.Since(attempt).Milliseconds(), "ready": false,
			"error": fmt.Sprint(err), "output_tail": out,
			"page_targets": pages, "page_query_error": fmt.Sprint(pagesErr),
		})
		if ctx.Err() != nil {
			return out, fmt.Errorf("browser initial preparation did not become ready: %w", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
