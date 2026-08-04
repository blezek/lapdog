VERSION ?= 0.1.0
MODULE  := github.com/blezek/lapdog
LDFLAGS := -X $(MODULE)/internal/version.Version=$(VERSION) -s -w

# CGO_ENABLED=0 is load-bearing, not a preference. It is why modernc.org/sqlite
# was chosen over the C bindings, and it is what allows the Windows binary to be
# cross-compiled from macOS with no mingw toolchain.
export CGO_ENABLED=0

DIST    := dist
BUNDLE  := internal/web/dist/index.html

# Everything the bundle is built from. Listed explicitly rather than as `web/*` so
# editing a source file rebuilds it while a stray file in web/ does not, and so
# node_modules is never scanned.
UI_SRC  := $(shell find web/src -type f 2>/dev/null) \
           $(wildcard web/index.html web/package.json web/package-lock.json) \
           $(wildcard web/vite.config.ts web/tsconfig*.json)

EXE     := $(DIST)/lapdog.exe
ZIP     := $(DIST)/lapdog-$(VERSION)-windows-amd64.zip
SETUP   := $(DIST)/lapdog-$(VERSION)-setup.exe

# Authenticode signing is optional. Absent a certificate the release still
# builds and emits unsigned artefacts with a warning, because a missing
# certificate must not block a development build.
SIGN_PKCS12   ?=
SIGN_PASSWORD ?=
TIMESTAMP_URL ?= http://timestamp.digicert.com

.PHONY: help test test-ci vet ui ui-clean ui-dev ci verify-embed build-windows build-ctl build-gen \
        fixtures dataset validate portable installer sign release tools clean

help:
	@echo "test           run the Go unit tests"
	@echo "test-ci        run the Go tests with the frontend bundle required"
	@echo "vet            run go vet"
	@echo "ui             build the frontend into internal/web/dist"
	@echo "ui-clean       rebuild the frontend from scratch"
	@echo "ci             what CI runs: vet, frontend, both test suites, cross-build"
	@echo "verify-embed   prove the interface is inside a Windows binary"
	@echo "ui-dev         run the Vite dev server against a local API"
	@echo "build-windows  cross-compile the tray app for Windows"
	@echo "build-ctl      build the lapdogctl development CLI"
	@echo "build-gen      build the dataset generator"
	@echo "fixtures       regenerate the committed test fixtures (~1.7 MB)"
	@echo "dataset        generate the full two-year dataset into .dataset (~250 MB, gitignored)"
	@echo "validate       replay .dataset back through decode, parse and classify"
	@echo "portable       zip the self-contained executable"
	@echo "installer      build the NSIS installer (needs: brew install makensis)"
	@echo "sign           Authenticode-sign the exe and installer (needs SIGN_PKCS12)"
	@echo "release        test, build, portable, installer, sign, checksums"
	@echo "tools          install the macOS packaging toolchain via brew"

test:
	go test ./...

# The bundle-dependent tests skip when the frontend has not been built, so that a
# clone without Node still runs the Go suite. That skip must not be reachable in
# CI or a release: this target builds the bundle first and makes its absence a
# failure rather than a skip.
test-ci: $(BUNDLE)
	LAPDOG_REQUIRE_BUNDLE=1 go test ./...

vet:
	go vet ./...

# The frontend bundle is generated, not committed: it is about a megabyte of
# minified JavaScript that changes wholesale on every UI edit, so committing it
# would bury real changes under regenerated noise. CI builds it, and so does any
# target that needs it — see BUNDLE below.
ui: $(BUNDLE)

# Force a rebuild even when the inputs look unchanged.
ui-clean:
	rm -rf internal/web/dist/assets internal/web/dist/index.html
	$(MAKE) ui

# A file target, not a phony one, so builds can depend on the bundle without
# rerunning npm on every invocation. index.html stands in for the whole bundle
# because the bundler always rewrites it: the asset names it references are
# content-hashed, so it cannot be stale while the assets are current.
#
# The previous assets are removed here rather than by the bundler. Vite's
# emptyOutDir would clear the whole directory including the tracked .gitkeep that
# keeps //go:embed compiling, so it is off; clearing only the generated paths gets
# the same freshness without deleting the placeholder.
$(BUNDLE): $(UI_SRC)
	rm -rf internal/web/dist/assets $(BUNDLE)
	cd web && npm ci && npm run build
	@test -d internal/web/dist/assets || { echo "ui: build produced no assets"; exit 1; }
	@test -f internal/web/dist/.gitkeep || { echo "ui: build removed the embed placeholder"; exit 1; }

# Vite serves the interface and proxies /api to a running backend, so the frontend
# hot-reloads while talking to real data:
#   ./dist/lapdogctl serve .dataset.db
ui-dev:
	cd web && npm run dev

# The tray app must be linked -H windowsgui so no console window appears.
#
# GOOS and GOARCH are set explicitly, and leaving them out was not a harmless
# omission. -H windowsgui only asks the linker for a PE header, so without them
# this produced a darwin/arm64 binary wearing a Windows header: `file` called it a
# Windows executable, it was named .exe, and it could not run on Windows at all.
# GOARCH matters too — windows/amd64 is the shipped target, and iRacing does not
# run on Windows on ARM, so an arm64 build would be useless even though it links.
build-windows: $(BUNDLE)
	GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui $(LDFLAGS)" -o $(EXE) ./cmd/lapdog

# lapdogctl is a separate binary precisely because a GUI-subsystem executable has
# no console and is therefore useless as a CLI. It is not shipped in releases.
build-ctl: $(BUNDLE)
	go build -ldflags "$(LDFLAGS)" -o dist/lapdogctl ./cmd/lapdogctl

build-gen:
	go build -ldflags "$(LDFLAGS)" -o dist/lapdog-gen ./cmd/lapdog-gen

# The committed fixture set is small and deterministic: the same seed and a fixed
# base date mean regenerating it produces byte-identical files, so it only
# changes when the generator genuinely changes.
fixtures: build-gen
	rm -rf testdata/fixtures
	./dist/lapdog-gen -fixtures -dir testdata/fixtures

# The full dataset is far too large to commit, so it is generated on demand.
dataset: build-gen
	rm -rf .dataset
	./dist/lapdog-gen -dir .dataset

validate: build-gen
	./dist/lapdog-gen -validate -dir .dataset

# The whole Windows toolchain runs on macOS, which is why the release needs no
# Windows machine. See the packaging spec, section 4.1.
tools:
	brew install makensis msitools osslsigncode

# The executable is genuinely self-contained, so a zip is a legitimate
# distribution channel rather than a degraded one.
portable: build-windows
	cd $(DIST) && zip -q -9 $(notdir $(ZIP)) lapdog.exe
	@echo "portable: $(ZIP)"

installer: build-windows
	@command -v makensis >/dev/null || { echo "makensis not found; run 'make tools'"; exit 1; }
	makensis -NOCD -V2 \
	  -DVERSION=$(VERSION) \
	  -DSRCEXE="$(CURDIR)/$(EXE)" \
	  -DOUTFILE="$(CURDIR)/$(SETUP)" \
	  packaging/windows/lapdog.nsi
	@echo "installer: $(SETUP)"

# Both the executable and the installer are signed. Signing the inner binary
# matters because someone running it from the portable zip never sees the
# installer's signature.
sign:
	@if [ -z "$(SIGN_PKCS12)" ]; then \
	  echo "WARNING: SIGN_PKCS12 is not set; leaving artefacts unsigned."; \
	  echo "         Unsigned installers trigger SmartScreen warnings."; \
	  exit 0; \
	fi
	@command -v osslsigncode >/dev/null || { echo "osslsigncode not found; run 'make tools'"; exit 1; }
	for f in $(EXE) $(SETUP); do \
	  [ -f "$$f" ] || continue; \
	  osslsigncode sign -pkcs12 "$(SIGN_PKCS12)" -pass "$(SIGN_PASSWORD)" \
	    -n "LapDog" -i "https://github.com/blezek/lapdog" \
	    -ts "$(TIMESTAMP_URL)" -in "$$f" -out "$$f.signed" && mv "$$f.signed" "$$f"; \
	done
	@echo "signed: $(EXE) $(SETUP)"

release: test-ci vet build-windows portable installer sign
	cd $(DIST) && shasum -a 256 lapdog.exe $(notdir $(ZIP)) $(notdir $(SETUP)) > SHA256SUMS
	@echo
	@echo "Release artefacts in $(DIST):"
	@cd $(DIST) && ls -lh lapdog.exe $(notdir $(ZIP)) $(notdir $(SETUP)) SHA256SUMS

# Proves the interface really is inside a Windows executable rather than read from
# disk at runtime, by finding strings that only exist in the bundle and icon set.
#
# It checks the shipped tray binary, which is the artefact users receive.
#
# The target is asserted rather than assumed. Greping for strings says nothing
# about what the binary was compiled for, and it passed happily on a darwin build
# that merely had a PE header attached. Go records the real values in the binary,
# so they are read back out of it.
verify-embed: build-windows
	@go version -m $(EXE) | grep -q "GOOS=windows" || { \
	  echo "verify-embed: $(EXE) was not built for Windows:"; go version -m $(EXE) | grep GOOS; exit 1; }
	@go version -m $(EXE) | grep -q "GOARCH=amd64" || { \
	  echo "verify-embed: $(EXE) is not amd64, the shipped target:"; go version -m $(EXE) | grep GOARCH; exit 1; }
	@for n in LapDog mdi-racing-helmet; do \
	  grep -qa "$$n" $(EXE) || { echo "verify-embed: $$n is missing from the binary"; exit 1; }; \
	done
	@echo "verify-embed: $(EXE) is windows/amd64 with the interface and icons inside"

# Mirrors .github/workflows/ci.yml so the same checks can be run before pushing.
ci: vet test-ci verify-embed
	cd web && npm run typecheck && npm run test
	GOOS=windows GOARCH=amd64 go build ./...
	@echo "ci: ok"

clean:
	rm -rf dist .dataset internal/web/dist/assets internal/web/dist/index.html
