package i18n

import "testing"

func TestLanguagePrecedenceAndFallback(t *testing.T) {
	t.Cleanup(func() { SetLang("en") })
	SetLang("zh-CN")
	if got := Language(); got != "zh-CN" {
		t.Fatalf("Language() = %q, want zh-CN", got)
	}
	SetLang("unsupported")
	if got := Language(); got != "en" {
		t.Fatalf("unsupported language = %q, want en", got)
	}
}

func TestCatalogsHaveMatchingKeys(t *testing.T) {
	for key := range translationMap["en"] {
		if _, ok := translationMap["zh-CN"][key]; !ok {
			t.Errorf("zh-CN catalog missing key %q", key)
		}
	}
	for key := range translationMap["zh-CN"] {
		if _, ok := translationMap["en"][key]; !ok {
			t.Errorf("en catalog missing key %q", key)
		}
	}
}
