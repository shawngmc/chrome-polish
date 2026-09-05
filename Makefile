.PHONY: build build-app build-relay tidy docs-serve docs-build clean

build: build-app build-relay

build-app:
	cd app && go build -o ../bin/chrome-polish ./cmd/chrome-polish

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
