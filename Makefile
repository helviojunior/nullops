
TARGET=./build
ARCHS=amd64 386 arm64
GOOS=windows linux darwin
PACKAGENAME="github.com/helviojunior/nullops"
BINNAME=nullops

FRONTEND_DIR=./frontend
FRONTEND_DIST=./frontend/dist
ASSET_BLOB=./pkg/assets/bundle.bin
# docker (default; host needs no Node) | local (uses npm on the host)
FRONTEND_BUILDER ?= docker
# 1 (default) = obfuscate the JavaScript shipped to the browser (javascript-obfuscator); 0 = plain minified build
OBFUSCATE_JS ?= 1
PEPPER_FILE=./build/.pepper

ifdef VER
	VER := $(VER)
else
	VER := dev
endif

GITHASH=`git rev-parse --short HEAD`
BUILDENV=`go version | cut -d' ' -f 3,4 | sed 's/ /_/g'`
BUILDTIME=`date -u +'%Y-%m-%dT%H:%M:%SZ'`


.PHONY: help local windows linux mac all clean hooks fmt frontend assets test test-frontend prepare get-version pepper screenshots

default: local

help:		   ## Show this help.
	@fgrep -h "##" $(MAKEFILE_LIST) | fgrep -v fgrep | sed -e 's/\\$$//' | sed -e 's/##//'

hooks:		   ## Install git hooks (gofmt pre-commit) via core.hooksPath
	@git config core.hooksPath .githooks ; \
	chmod +x .githooks/* 2>/dev/null || true ; \
	echo "Git hooks installed (core.hooksPath -> .githooks)."

fmt:		   ## Format all Go code with gofmt -s -w
	@gofmt -s -w . ; \
	echo "Done."

frontend:	   ## Build the web front-end (html/js/css) into frontend/dist
	@echo "Building front-end ($(FRONTEND_BUILDER))..." ; \
	rm -rf ./frontend/dist ; \
	SCRIPT=build ; if [ "$(OBFUSCATE_JS)" = "1" ]; then SCRIPT=build:obfuscated ; fi ; \
	if [ "$(FRONTEND_BUILDER)" = "local" ]; then \
		( cd $(FRONTEND_DIR) && (npm ci --no-audit --no-fund || npm install --no-audit --no-fund) && npm run $$SCRIPT ) || exit 1 ; \
	else \
		DOCKER_BUILDKIT=1 docker build --build-arg BUILD_SCRIPT=$$SCRIPT --target export --output type=local,dest=$(FRONTEND_DIST) $(FRONTEND_DIR) || exit 1 ; \
	fi ; \
	test -f $(FRONTEND_DIST)/index.html || { echo "front-end build produced no index.html" ; exit 1 ; } ; \
	echo "Front-end ready in $(FRONTEND_DIST)."

pepper:		   ## Generate a random per-build obfuscation key (kept until `make clean`)
	@mkdir -p ./build ; \
	if [ ! -s $(PEPPER_FILE) ]; then \
		head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n' > $(PEPPER_FILE) ; \
		echo "Generated a new per-build obfuscation key." ; \
	fi

assets: frontend pepper   ## Seal the front-end into the obfuscated blob embedded by Go
	@go run ./tools/assetpack -in $(FRONTEND_DIST) -out $(ASSET_BLOB) -pepper $$(cat $(PEPPER_FILE))

test:		   ## Run Go unit tests (needs the asset blob: run `make assets` once)
	@go test -ldflags "-X=${PACKAGENAME}/pkg/assets/bundle.BuildPepper=$$(cat $(PEPPER_FILE) 2>/dev/null)" ./...

test-frontend:	   ## Run the front-end unit tests (vitest)
	@if [ "$(FRONTEND_BUILDER)" = "local" ]; then \
		( cd $(FRONTEND_DIR) && (npm ci --no-audit --no-fund || npm install --no-audit --no-fund) && npm test ) || exit 1 ; \
	else \
		DOCKER_BUILDKIT=1 docker build --target test $(FRONTEND_DIR) || exit 1 ; \
	fi

PLAYWRIGHT_IMAGE ?= mcr.microsoft.com/playwright/python:v1.47.0-jammy

screenshots: prepare	## Regenerate the README screenshots in docs/images (Docker + Playwright)
	@W=$$(mktemp -d) ; \
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$$(cat ${TARGET}/.ldflags)" -o $$W/nullops . || exit 1 ; \
	bash tools/screenshots/make-demo.sh $$W/demo || exit 1 ; \
	cp tools/screenshots/shoot.py tools/screenshots/run.sh $$W/ ; mkdir -p $$W/out ; \
	docker run --rm -v $$W:/work $(PLAYWRIGHT_IMAGE) bash /work/run.sh || exit 1 ; \
	mkdir -p docs/images && cp $$W/out/*.png docs/images/ ; \
	echo "Screenshots updated in docs/images (work dir: $$W)."

get-version:
	@mkdir -p ${TARGET} ; \
	if [ "$(VER)" = "dev" ]; then \
		if [ -f ${TARGET}/.version ]; then \
			echo "Using cached release tag from .version" ; \
		else \
			echo "Fetching latest release tag from GitHub..." ; \
			NAME=`echo ${PACKAGENAME} | sed 's/github.com//g'` ; \
			curl -s https://api.github.com/repos$${NAME}/releases/latest \
				| grep '"tag_name":' | head -n 1 | grep -oE '[0-9\.]+' > ${TARGET}/.version ; \
			[ -s ${TARGET}/.version ] || echo "0.0.0" > ${TARGET}/.version ; \
			echo "Saved release tag to .version" ; \
		fi ; \
		VER1=`cat ${TARGET}/.version` ; \
		echo "$${VER1}-dev" > ${TARGET}/.verstr ; \
	else \
		echo "VER is set to '$(VER)', skipping release tag fetch."; \
		echo "$(VER)" > ${TARGET}/.verstr ; \
	fi ; \
	echo "Version: $$(cat ${TARGET}/.verstr)"; \
	echo "-s -w \
-X=${PACKAGENAME}/internal/version.Version=$$(cat ${TARGET}/.verstr) \
-X=${PACKAGENAME}/internal/version.GitHash=${GITHASH} \
-X=${PACKAGENAME}/internal/version.GoBuildEnv=${BUILDENV} \
-X=${PACKAGENAME}/internal/version.GoBuildTime=${BUILDTIME} \
-X=${PACKAGENAME}/pkg/assets/bundle.BuildPepper=$$(cat $(PEPPER_FILE)) \
" > ${TARGET}/.ldflags

prepare: assets get-version

windows: prepare	## Make Windows x86 and x64 Binaries
	@for ARCH in ${ARCHS}; do \
	    if [ "$${ARCH}" != "386" ]; then \
			echo "Building for windows $${ARCH}.." ;\
			CGO_ENABLED=0 GOOS=windows GOARCH=$${ARCH} go build -trimpath -a -ldflags "$$(cat ${TARGET}/.ldflags)" -o ${TARGET}/${BINNAME}_windows_$${ARCH}.exe || exit 1 ;\
		fi; \
	done; \
	echo "Done."

linux: prepare	## Make Linux x86 and x64 Binaries
	@for ARCH in ${ARCHS}; do \
		echo "Building for linux $${ARCH}..." ; \
		CGO_ENABLED=0 GOOS=linux GOARCH=$${ARCH} go build -trimpath -a -ldflags "$$(cat ${TARGET}/.ldflags)" -o ${TARGET}/${BINNAME}_linux_$${ARCH} || exit 1 ;\
	done; \
	echo "Done."

mac: prepare	## Make Darwin (Mac) x86 and x64 Binaries
	@for ARCH in ${ARCHS}; do \
		if [ "$${ARCH}" != "386" ]; then \
			echo "Building for mac $${ARCH}..." ; \
			CGO_ENABLED=0 GOOS=darwin GOARCH=$${ARCH} go build -trimpath -a -ldflags "$$(cat ${TARGET}/.ldflags)" -o ${TARGET}/${BINNAME}_darwin_$${ARCH} || exit 1 ;\
		fi;\
	done; \
	echo "Done."

clean: ## Delete any binaries and generated front-end files
	@rm -rf ./build ./frontend/dist ./pkg/assets/bundle.bin ; \
	go clean -i -n ${PACKAGENAME} ; \
	echo "Done."

local: prepare	## Build for the local OS/architecture
	@OS=$$(uname | tr '[:upper:]' '[:lower:]') ; \
	ARCH=$$(if [ "$$(uname -m)" = "x86_64" ]; then echo "amd64"; else echo "arm64"; fi) ; \
	echo "Building for $$OS ($$ARCH)..." ; \
	CGO_ENABLED=0 GOOS=$$OS GOARCH=$$ARCH go build -trimpath -ldflags "$$(cat ${TARGET}/.ldflags)" -o ${TARGET}/${BINNAME}_$$OS\_$$ARCH || exit 1 ; \
	echo "Done."

all: ## Make Windows, Linux and Mac x86/x64 Binaries
all: clean windows linux mac
