GO := go
DIST_DIR := dist
WASM_EXEC := $(shell $(GO) env GOROOT)/lib/wasm/wasm_exec.js

.PHONY: help test run build desktop web mobile serve-web clean

help:
	@echo "Targets:"
	@echo "  make test         - run all tests"
	@echo "  make run          - run local sample (1_adventure)"
	@echo "  make desktop      - build desktop binaries (linux/darwin/windows)"
	@echo "  make web          - build wasm web bundle"
	@echo "  make build        - build desktop + web"
	@echo "  make mobile       - build Android/iOS packages with gomobile"
	@echo "  make serve-web    - serve dist/web at :8080"
	@echo "  make clean        - remove dist artifacts"

test:
	$(GO) test ./...

run:
	$(GO) run ./cmd/erago -base ./era_files/1_adventure -entry TITLE

build: desktop web

desktop: \
	$(DIST_DIR)/linux-amd64/erago \
	$(DIST_DIR)/linux-arm64/erago \
	$(DIST_DIR)/darwin-amd64/erago \
	$(DIST_DIR)/darwin-arm64/erago \
	$(DIST_DIR)/windows-amd64/erago.exe \
	$(DIST_DIR)/windows-arm64/erago.exe

$(DIST_DIR)/linux-amd64/erago:
	@mkdir -p $(@D)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-s -w" -o $@ ./cmd/erago

$(DIST_DIR)/linux-arm64/erago:
	@mkdir -p $(@D)
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-s -w" -o $@ ./cmd/erago

$(DIST_DIR)/darwin-amd64/erago:
	@mkdir -p $(@D)
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-s -w" -o $@ ./cmd/erago

$(DIST_DIR)/darwin-arm64/erago:
	@mkdir -p $(@D)
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-s -w" -o $@ ./cmd/erago

$(DIST_DIR)/windows-amd64/erago.exe:
	@mkdir -p $(@D)
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-s -w" -o $@ ./cmd/erago

$(DIST_DIR)/windows-arm64/erago.exe:
	@mkdir -p $(@D)
	GOOS=windows GOARCH=arm64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-s -w" -o $@ ./cmd/erago

web: \
	$(DIST_DIR)/web/erago.wasm \
	$(DIST_DIR)/web/wasm_exec.js \
	$(DIST_DIR)/web/index.html

$(DIST_DIR)/web/erago.wasm: build/web/main_js.go
	@mkdir -p $(@D)
	GOOS=js GOARCH=wasm CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-s -w" -o $@ ./build/web

$(DIST_DIR)/web/wasm_exec.js:
	@mkdir -p $(@D)
	cp -f "$(WASM_EXEC)" $@

$(DIST_DIR)/web/index.html: build/web/index.html
	@mkdir -p $(@D)
	cp -f $< $@

mobile:
	@command -v gomobile >/dev/null 2>&1 || (echo "gomobile is required: go install golang.org/x/mobile/cmd/gomobile@latest" && exit 1)
	@mkdir -p $(DIST_DIR)/mobile
	gomobile init
	rm -f $(DIST_DIR)/mobile/erago_mobile.aar
	rm -rf $(DIST_DIR)/mobile/EragoMobile.xcframework
	gomobile bind -target=android -o $(DIST_DIR)/mobile/erago_mobile.aar ./build/mobile
	gomobile bind -target=ios -o $(DIST_DIR)/mobile/EragoMobile.xcframework ./build/mobile

serve-web: web
	cd $(DIST_DIR)/web && python3 -m http.server 8080

clean:
	rm -rf $(DIST_DIR)
