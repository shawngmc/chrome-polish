.PHONY: build build-app build-relay build-cdp-probe build-scan-probe build-dom-probe tidy docs-serve docs-build clean

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

tidy:
	cd app && go mod tidy
	cd relay && go mod tidy

docs-serve:
	cd docs && hugo server

docs-build:
	cd docs && hugo --minify

clean:
	rm -rf bin
