package main

import (
	"strings"
	"testing"
)

// 自检：layout 组合 + 缓存渲染 + 自定义函数都正常工作。
func TestRenderIndex(t *testing.T) {
	html, err := registry.LoadFS(templates,
		"templates/layout.html",
		"templates/pages/index.html",
	).Render(PageData{Title: "Index", Name: "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "Hello, Ada!") || !strings.Contains(html, "<title>Index</title>") {
		t.Fatalf("unexpected html:\n%s", html)
	}

	if _, err := registry.LoadFS(templates, "templates/pages/about.html").Render(PageData{Title: "about"}); err != nil {
		t.Fatal(err)
	}
}
