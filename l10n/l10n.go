package l10n

import (
	"os"
	"strings"

	"golang.org/x/text/language"
)

// 基本フレーズ - 翻訳フレーズのマップ
type LexiconMap map[string]string

// 言語 - 基本フレーズ - 翻訳フレーズのマップ
type WorldMap map[string]LexiconMap

var (
	// 現在アクティブな言語
	Language = "en"

	// テスト中の言語固定フラグ
	forcedLanguage   string
	isLanguageForced bool
)

func DetectLanguage() {
	// 言語が強制設定されている場合はそれを使用
	if isLanguageForced {
		Language = forcedLanguage
		return
	}

	// テスト実行中かチェック（テスト中は英語に固定）
	if isTestMode() {
		Language = "en"
		return
	}

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

// isTestMode checks if the code is running in test mode
func isTestMode() bool {
	// Goのテスト実行中は、実行可能ファイル名に.testが含まれる
	// または、引数にtest関連のフラグが含まれる
	for _, arg := range os.Args {
		if strings.Contains(arg, ".test") ||
			strings.Contains(arg, "-test.") ||
			strings.HasSuffix(arg, "_test") {
			return true
		}
	}
	return false
}

// ForceLanguage forces the language to a specific value (primarily for testing)
func ForceLanguage(lang string) {
	forcedLanguage = lang
	isLanguageForced = true
	Language = lang
}

// ResetLanguage resets language detection to automatic mode
func ResetLanguage() {
	isLanguageForced = false
	forcedLanguage = ""
	DetectLanguage()
}

// GetCurrentLanguage returns the currently active language
func GetCurrentLanguage() string {
	return Language
}

func init() {
	DetectLanguage()
}
