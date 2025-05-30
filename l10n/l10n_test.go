package l10n

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsTestMode(t *testing.T) {
	// テスト実行中はisTestMode()がtrueを返すことを確認
	assert.True(t, isTestMode(), "isTestMode should return true during test execution")
}

func TestDetectLanguage_TestMode(t *testing.T) {
	// テスト実行中は環境変数に関係なく英語が選択されることを確認

	// 現在の環境変数を保存
	originalLang := os.Getenv("LANG")
	originalLanguage := os.Getenv("LANGUAGE")

	// 日本語の環境変数を設定
	os.Setenv("LANG", "ja_JP.UTF-8")
	os.Setenv("LANGUAGE", "ja:en")

	// テスト実行中なので英語が選択されることを確認
	DetectLanguage()
	assert.Equal(t, "en", Language, "Language should be 'en' during test execution regardless of environment variables")

	// 環境変数を復元
	if originalLang != "" {
		os.Setenv("LANG", originalLang)
	} else {
		os.Unsetenv("LANG")
	}
	if originalLanguage != "" {
		os.Setenv("LANGUAGE", originalLanguage)
	} else {
		os.Unsetenv("LANGUAGE")
	}
}

func TestForceLanguage(t *testing.T) {
	// 強制言語設定のテスト

	// 初期状態を保存
	originalLanguage := Language
	originalIsForced := isLanguageForced
	originalForcedLanguage := forcedLanguage

	defer func() {
		// テスト後に状態を復元
		Language = originalLanguage
		isLanguageForced = originalIsForced
		forcedLanguage = originalForcedLanguage
	}()

	// 日本語に強制設定
	ForceLanguage("ja")
	assert.Equal(t, "ja", Language, "Language should be 'ja' after ForceLanguage")
	assert.Equal(t, "ja", GetCurrentLanguage(), "GetCurrentLanguage should return 'ja'")
	assert.True(t, isLanguageForced, "isLanguageForced should be true")

	// 強制設定中はDetectLanguageを呼んでも変わらない
	DetectLanguage()
	assert.Equal(t, "ja", Language, "Language should remain 'ja' even after DetectLanguage when forced")

	// 強制設定をリセット
	ResetLanguage()
	assert.False(t, isLanguageForced, "isLanguageForced should be false after reset")
	// テスト中なので英語に戻る
	assert.Equal(t, "en", Language, "Language should be 'en' after reset during test")
}

func TestTranslation(t *testing.T) {
	// 翻訳機能のテスト

	// テスト用の翻訳を登録
	Register("ja", LexiconMap{
		"Hello":   "こんにちは",
		"Goodbye": "さようなら",
	})

	// 初期状態を保存
	originalLanguage := Language
	defer func() {
		Language = originalLanguage
	}()

	// 英語モード（翻訳なし）
	Language = "en"
	assert.Equal(t, "Hello", T("Hello"), "T should return original phrase in English mode")
	assert.Equal(t, "Unknown phrase", T("Unknown phrase"), "T should return original phrase for unknown phrases")

	// 日本語モード（翻訳あり）
	Language = "ja"
	assert.Equal(t, "こんにちは", T("Hello"), "T should return Japanese translation")
	assert.Equal(t, "さようなら", T("Goodbye"), "T should return Japanese translation")
	assert.Equal(t, "Unknown phrase", T("Unknown phrase"), "T should return original phrase for untranslated phrases")
}

func TestRegister(t *testing.T) {
	// 翻訳登録機能のテスト

	// 新しい言語を登録
	Register("fr", LexiconMap{
		"Hello":     "Bonjour",
		"Thank you": "Merci",
	})

	// 既存の言語に追加登録
	Register("fr", LexiconMap{
		"Goodbye": "Au revoir",
	})

	// 登録された翻訳を確認
	frLexicon, exists := World["fr"]
	require.True(t, exists, "French lexicon should exist")
	assert.Equal(t, "Bonjour", frLexicon["Hello"], "French translation for Hello should be registered")
	assert.Equal(t, "Merci", frLexicon["Thank you"], "French translation for Thank you should be registered")
	assert.Equal(t, "Au revoir", frLexicon["Goodbye"], "French translation for Goodbye should be registered via merge")

	// 元の日本語翻訳が影響を受けていないことを確認
	_, exists = World["ja"]
	require.True(t, exists, "Japanese lexicon should still exist")

	// フランス語モードでテスト
	originalLanguage := Language
	defer func() {
		Language = originalLanguage
	}()

	Language = "fr"
	assert.Equal(t, "Bonjour", T("Hello"), "T should return French translation")
	assert.Equal(t, "Au revoir", T("Goodbye"), "T should return French translation")
	assert.Equal(t, "Unknown", T("Unknown"), "T should return original phrase for untranslated phrases")
}
