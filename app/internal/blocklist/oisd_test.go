package blocklist

import (
	"context"
	"testing"

	"fyne.io/fyne/v2/test"
)

func TestShouldPromptForDefaults(t *testing.T) {
	t.Run("prompts when nothing is set and no oisd source exists", func(t *testing.T) {
		m := NewManager(t.TempDir())
		prefs := test.NewApp().Preferences()
		if !ShouldPromptForDefaults(m, prefs) {
			t.Error("expected a prompt for a fresh manager with no preference set")
		}
	})

	t.Run("does not prompt once declined permanently", func(t *testing.T) {
		m := NewManager(t.TempDir())
		prefs := test.NewApp().Preferences()
		prefs.SetBool(PrefSkipOisdPrompt, true)
		if ShouldPromptForDefaults(m, prefs) {
			t.Error("expected no prompt once the skip preference is set")
		}
	})

	t.Run("does not prompt once an oisd source already exists", func(t *testing.T) {
		m := NewManager(t.TempDir())
		m.fetch = stubFetch([]byte("evil.com\n"), nil)
		if _, err := m.AddURL(context.Background(), OisdBigURL, OisdBigName); err != nil {
			t.Fatalf("AddURL: %v", err)
		}
		prefs := test.NewApp().Preferences()
		if ShouldPromptForDefaults(m, prefs) {
			t.Error("expected no prompt once the oisd big source already exists")
		}
	})
}
