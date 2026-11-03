// Package i18n loads translation files and localizes messages | 加载翻译文件并本地化消息.
//
// # Notes | 说明
//
// A thin wrapper around nicksnyder/go-i18n: New builds one bundle in a single
// format (yaml by default, toml or json optional) and loads message files from
// disk paths, directories or an embed.FS. Each instance holds one language;
// Select returns a new instance sharing the same bundle. Load failures are
// silently ignored and a missing translation falls back to the message ID, so
// a broken file degrades to untranslated output instead of an error.
// 基于 nicksnyder/go-i18n 的薄封装：New 按单一格式（默认 yml，可选
// toml/json）构建 bundle，从磁盘文件、目录或 embed.FS 加载消息。每个实例
// 一种语言；Select 返回共享同一 bundle 的新实例。加载失败被静默忽略，
// 缺失翻译回退到消息 ID——坏文件只会退化为未翻译输出，而不是报错。
//
// # Features | 功能
//
// - T/E and LocalizeT/LocalizeE message lookups | T/E 与 LocalizeT/LocalizeE 查询
// - WithLanguage/WithFormat/WithFiles/WithFS options | 语言/格式/文件/嵌入选项
// - NewContext/FromContext carry the translator | 经 context 传递翻译器
//
// # Example | 示例
//
//	i := i18n.New(i18n.WithLanguage(language.Chinese), i18n.WithFiles("locales"))
//	fmt.Println(i.T("welcome"))
package i18n // import "github.com/superproj/onex/pkg/i18n"
