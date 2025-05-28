package l10n

import (
	"os"

	"golang.org/x/text/language"
)

// 基本フレーズ - 翻訳フレーズのマップ
type LexiconMap map[string]string

// 言語 - 基本フレーズ - 翻訳フレーズのマップ
type WorldMap map[string]LexiconMap

var (
	// 現在アクティブな言語
	Language = "en"
)

func DetectLanguage() {
	// 環境変数から言語を判定
	langs := []string{
		os.Getenv("LANGUAGE"),
		os.Getenv("LC_ALL"),
		os.Getenv("LC_MESSAGES"),
		os.Getenv("LANG"),
	}

	matcher := language.NewMatcher([]language.Tag{
		language.Japanese,
		language.English,
	})

	// デフォルトは英語だが、環境変数から日本語が指定されていたら日本語にする
	Language = "en"
	for _, l := range langs {
		if l != "" {
			tag, _ := language.MatchStrings(matcher, l)
			if tag == language.Japanese {
				Language = "ja"
				break
			}
		}
	}
}

// フレーズマップに翻訳フレーズをマージ的に登録する
// ShotやLambdaなどの拡張アプリケーションはRegisterを利用して
// 個別の翻訳フレーズを拡張することで透過的にi10nを利用できる
func Register(lang string, lex LexiconMap) {
	l, ok := World[lang]
	if !ok {
		World[lang] = lex
		return
	}

	for k, v := range lex {
		l[k] = v
	}
}

// フレーズを翻訳する
func T(phrase string) string {
	l, ok := World[Language]
	if !ok {
		return phrase
	}

	t, ok := l[phrase]
	if !ok {
		return phrase
	}

	return t
}

var (
	// グローバルなフレーズマップ
	World = WorldMap{
		"ja": LexiconMap{},
	}
)

func init() {
	DetectLanguage()
}
