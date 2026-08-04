VERSION ?= 0.1.0
MODULE  := github.com/blezek/lapdog
LDFLAGS := -X $(MODULE)/internal/version.Version=$(VERSION) -s -w

# CGO_ENABLED=0 is load-bearing, not a preference. It is why modernc.org/sqlite
# was chosen over the C bindings, and it is what allows the Windows binary to be
# cross-compiled from macOS with no mingw toolchain.
export CGO_ENABLED=0

DIST    := dist
EXE     := $(DIST)/lapdog.exe
ZIP     := $(DIST)/lapdog-$(VERSION)-windows-amd64.zip
SETUP   := $(DIST)/lapdog-$(VERSION)-setup.exe

# Authenticode signing is optional. Absent a certificate the release still
# builds and emits unsigned artefacts with a warning, because a missing
# certificate must not block a development build.
SIGN_PKCS12   ?=
SIGN_PASSWORD ?=
TIMESTAMP_URL ?= http://timestamp.digicert.com

.PHONY: help test vet ui ui-dev build-windows build-ctl build-gen fixtures dataset validate \
        portable installer sign release tools clean

help:
	@echo "test           run the unit tests"
	@echo "vet            run go vet"
	@echo "ui             build the frontend into internal/web/dist"
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

vet:
	go vet ./...

# The built bundle is committed to internal/web/dist so that `go build` works
# without a Node toolchain, and so a release can be reproduced from the Go source
# alone. Rerun this after changing anything under web/.
ui:
	cd web && npm ci && npm run build

# Vite serves the interface and proxies /api to a running backend, so the frontend
# hot-reloads while talking to real data:
#   ./dist/lapdogctl serve .dataset.db
ui-dev:
	cd web && npm run dev

# The tray app must be linked -H windowsgui so no console window appears.
build-windows:
	go build -ldflags "-H windowsgui $(LDFLAGS)" -o dist/lapdog.exe ./cmd/lapdog

# lapdogctl is a separate binary precisely because a GUI-subsystem executable has
# no console and is therefore useless as a CLI. It is not shipped in releases.
build-ctl:
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

release: test vet ui build-windows portable installer sign
	cd $(DIST) && shasum -a 256 lapdog.exe $(notdir $(ZIP)) $(notdir $(SETUP)) > SHA256SUMS
	@echo
	@echo "Release artefacts in $(DIST):"
	@cd $(DIST) && ls -lh lapdog.exe $(notdir $(ZIP)) $(notdir $(SETUP)) SHA256SUMS

clean:
	rm -rf dist .dataset
