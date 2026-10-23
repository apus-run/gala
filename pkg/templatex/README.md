# templatex

Thin wrapper around the standard `html/template` package: a concurrency-safe
registry that parses a template set on first use and caches the result in an
LRU cache. It exists for templates that are only known at **runtime** — JSVM
plugin HTML rendering, admin-configured pages, strings built on the fly —
where a build-time solution cannot see the template at all.

If every template is known when you compile, prefer
[templ](https://templ.guide): it generates type-checked Go code with no
runtime parsing. templatex is the tool for the other case.

## Usage

```go
registry := templatex.NewRegistry().AddFuncs(map[string]any{
    "toUpper": strings.ToUpper,
})

// The file set is parsed once, then served from cache on every later call.
html, err := registry.LoadFiles("layout.html", "content.html").
    Render(map[string]any{"name": "John"})
```

`LoadString(text)` caches an inline template keyed by its own text;
`LoadFS(fsys, globs...)` caches an `fs.FS` + glob pair (e.g. `embed.FS`).
`Renderer.Render` returns both parse and execution errors.

A complete HTTP example with layout composition, embedded templates and
typed data: [examples/webdemo](examples/webdemo).

```
cd components/templatex && go run ./examples/webdemo   # http://localhost:8080
```

## Known limits

- **Parse errors are cached.** A template set that fails to parse is cached
  with its error and keeps returning it; fixing the file on disk does not
  help until the process restarts (the 64k-entry LRU never evicts in
  practice). `Registry.Reset()` drops all cached entries so the next Load
  re-parses — the escape hatch for development.
- **AddFuncs is init-only.** Register funcs before the first Load; the
  registry's concurrency safety covers rendering, not later func additions.
- **First-load race.** Two goroutines loading the same uncached key may both
  parse it; the last write wins. Wasted work, not a correctness issue.
- **html/template only.** The package doc mentions `text/template`, but the
  registry parses everything as HTML templates. Escaping applies.
