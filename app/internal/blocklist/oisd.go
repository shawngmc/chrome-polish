package blocklist

import "fyne.io/fyne/v2"

// OisdBigURL and OisdNSFWURL are oisd.nl's two most broadly useful lists
// (see https://oisd.nl/faq): "big" for general malware/ad/tracker
// coverage, "nsfw" for adult content. Both are GPLv3-licensed, so this
// app fetches and caches them at runtime instead of bundling them (see
// PreferSkipOisdPrompt / ShouldPromptForDefaults).
const (
	OisdBigName  = "oisd big"
	OisdBigURL   = "https://big.oisd.nl"
	OisdNSFWName = "oisd nsfw"
	OisdNSFWURL  = "https://nsfw.oisd.nl"
)

// PrefSkipOisdPrompt is the fyne.Preferences key set when a person
// declines the startup offer to download the oisd lists and asks not to
// be asked again.
const PrefSkipOisdPrompt = "blocklist.skipOisdPrompt"

// ShouldPromptForDefaults reports whether the app should offer to
// download the oisd default lists on launch: it hasn't been declined
// permanently, and neither oisd list is already a known source (whether
// it fetched successfully or not — once added, refreshing it is the
// manager UI's job, not this prompt's).
func ShouldPromptForDefaults(mgr *Manager, prefs fyne.Preferences) bool {
	if prefs.Bool(PrefSkipOisdPrompt) {
		return false
	}
	for _, s := range mgr.Sources() {
		if s.Location == OisdBigURL || s.Location == OisdNSFWURL {
			return false
		}
	}
	return true
}
