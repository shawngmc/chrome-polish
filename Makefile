.PHONY: build build-app build-relay build-cdp-probe build-scan-probe build-dom-probe tidy docs-serve docs-build package clean

APP_ID      ?= com.shawngmc.chromepolish
APP_VERSION ?= 0.1.0
APP_BUILD   ?= 1

build: build-app build-relay

build-app:
	cd app && go build -o ../bin/chrome-polish ./cmd/chrome-polish

build-cdp-probe:
	cd app && go build -o ../bin/cdp-probe ./cmd/cdp-probe

build-scan-probe:
	cd app && go build -o ../bin/scan-probe ./cmd/scan-probe

build-dom-probe:
	cd app && go build -o ../bin/dom-probe ./cmd/dom-probe

build-relay:
	cd relay && go build -o ../bin/chrome-polish-relay ./cmd/chrome-polish-relay

# Packages the app for whatever OS/arch this runs on, into
# app/cmd/chrome-polish/ (gitignored, not moved elsewhere). Fyne apps use
# CGO for native graphics, so — per DESIGN.md section 6 — packaging for a
# DIFFERENT OS generally needs to happen on that OS, not cross-compiled
# from here; see .github/workflows/build.yml for the CI matrix that does
# this for macOS/Linux/Windows on tagged releases.
package:
	cd app/cmd/chrome-polish && go run fyne.io/fyne/v2/cmd/fyne package \
		--icon ../../Icon.png \
		--name "Chrome Polish" \
		--appID $(APP_ID) \
		--appVersion $(APP_VERSION) \
		--appBuild $(APP_BUILD) \
		--release

tidy:
	cd app && go mod tidy
	cd relay && go mod tidy

docs-serve:
	cd docs && hugo server

docs-build:
	cd docs && hugo --minify

clean:
	rm -rf bin
