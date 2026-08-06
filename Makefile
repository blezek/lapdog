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

EXE      := $(DIST)/lapdog.exe
CTLEXE   := $(DIST)/lapdogctl.exe
PORTABLE := $(DIST)/lapdog-$(VERSION)-portable.zip
SETUP    := $(DIST)/lapdog-$(VERSION)-setup.exe

# Authenticode signing is optional. Absent a certificate the release still
# builds and emits unsigned artefacts with a warning, because a missing
# certificate must not block a development build.
# The development dataset. Overridable so a real database can be inspected with the
# same target: make run-ctl DEV_DB=~/.local/share/lapdog/lapdog.db
DEV_DB   ?= .dataset.db
DEV_PORT ?= 47047

SIGN_PKCS12   ?=
SIGN_PASSWORD ?=
TIMESTAMP_URL ?= http://timestamp.digicert.com

.PHONY: help test test-ci vet fmt-check ui ui-clean ui-dev ci verify-embed run-ctl dataset-db \
        build-windows build-windows-ctl build-ctl build-gen \
        fixtures dataset validate portable installer sign release tools clean

help:
	@echo "test           run the Go unit tests"
	@echo "test-ci        run the Go tests with the frontend bundle required"
	@echo "vet            run go vet"
	@echo "fmt-check      fail if any file is not gofmt-formatted"
	@echo "ui             build the frontend into internal/web/dist"
	@echo "ui-clean       rebuild the frontend from scratch"
	@echo "ci             what CI runs: fmt check, vet, frontend, both test suites, cross-build"
	@echo "verify-embed   prove the interface is inside a Windows binary"
	@echo "ui-dev         run the Vite dev server against a local API"
	@echo "run-ctl        serve $(DEV_DB) on port $(DEV_PORT) for local testing"
	@echo "dataset-db     ingest the generated captures into $(DEV_DB)"
	@echo "build-windows  cross-compile the tray app for Windows"
	@echo "build-ctl      build the lapdogctl CLI for this machine"
	@echo "build-windows-ctl  cross-compile the lapdogctl CLI for Windows"
	@echo "build-gen      build the dataset generator"
	@echo "fixtures       regenerate the committed test fixtures (~1.7 MB)"
	@echo "dataset        generate the full two-year dataset into .dataset (~250 MB, gitignored)"
	@echo "validate       replay .dataset back through decode, parse and classify"
	@echo "portable       zip both self-contained executables"
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

# gofmt -l lists unformatted files and exits 0 regardless, so the failure has to
# come from the output being non-empty rather than from the exit code. This is
# what would have caught the unformatted files that reached main unnoticed.
fmt-check:
	@files="$$(gofmt -l ./internal ./cmd)"; \
	if [ -n "$$files" ]; then \
	  echo "fmt-check: these files are not gofmt-formatted:"; \
	  echo "$$files"; \
	  exit 1; \
	fi

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
# no console and is therefore useless as a CLI.
build-ctl: $(BUNDLE)
	go build -ldflags "$(LDFLAGS)" -o dist/lapdogctl ./cmd/lapdogctl

# The Windows build of the same CLI, shipped alongside the tray app.
#
# No -H windowsgui here, and that is the whole point: lapdogctl is a console
# program, and linking it into the GUI subsystem would give it nowhere to print.
#
# It ships because the machine that needs diagnosing is a Windows machine with a
# simulator and no development environment. `lapdogctl inspect` on a capture is how
# a telemetry problem gets identified there, and requiring a Go toolchain to obtain
# it puts the tool out of reach exactly when it is needed.
build-windows-ctl: $(BUNDLE)
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(CTLEXE) ./cmd/lapdogctl

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

# Ingest the generated captures into a database.
#
# This is the step between `make dataset` and anything that reads data: the
# generator writes capture files, and the database is what replaying them produces.
# Going through ingest rather than writing rows directly is the point — it means the
# development data has been through the same decode, classify and accounting path as
# a real session, so a bug there shows up here rather than only on a race weekend.
dataset-db: build-ctl
	@test -d .dataset || { \
	  echo "dataset-db: .dataset does not exist; run 'make dataset' first"; \
	  echo "            (generates about 250 MB of captures, and is gitignored)"; exit 1; }
	rm -f $(DEV_DB) $(DEV_DB)-wal $(DEV_DB)-shm
	./dist/lapdogctl ingest .dataset $(DEV_DB)
	./dist/lapdogctl summary $(DEV_DB)

# Serve a database locally, for looking at the interface with real data in it.
#
# This is the quickest way to see the UI: the synthetic dataset covers two years of
# sessions, so every chart has something in it. The tray application is the other
# way to run the interface, but on a development machine it has no simulator to read
# and so shows an empty database.
run-ctl: build-ctl
	@test -f $(DEV_DB) || { \
	  echo "run-ctl: $(DEV_DB) does not exist. To create it:"; \
	  echo "           make dataset      # generate captures (~250 MB, a few minutes)"; \
	  echo "           make dataset-db   # replay them into $(DEV_DB)"; \
	  echo "         Or point at another database: make run-ctl DEV_DB=path/to.db"; exit 1; }
	@echo "run-ctl: http://127.0.0.1:$(DEV_PORT)  (Ctrl-C to stop)"
	./dist/lapdogctl serve $(DEV_DB) $(DEV_PORT)

# The whole Windows toolchain runs on macOS, which is why the release needs no
# Windows machine. See the packaging spec, section 4.1.
tools:
	brew install makensis msitools osslsigncode

# Both executables are genuinely self-contained, so a zip is a legitimate
# distribution channel rather than a degraded one.
#
# The archive is removed first rather than written into. zip adds to an existing
# archive and replaces only the entries it is given, so a rebuild after renaming or
# dropping a file would leave the old entry in place — a stale binary shipping
# beside a current one, with nothing to indicate it.
portable: build-windows build-windows-ctl
	rm -f $(PORTABLE)
	cd $(DIST) && zip -q -9 $(notdir $(PORTABLE)) lapdog.exe lapdogctl.exe
	@echo "portable: $(PORTABLE)"

installer: build-windows
	@command -v makensis >/dev/null || { echo "makensis not found; run 'make tools'"; exit 1; }
	makensis -NOCD -V2 \
	  -DVERSION=$(VERSION) \
	  -DSRCEXE="$(CURDIR)/$(EXE)" \
	  -DOUTFILE="$(CURDIR)/$(SETUP)" \
	  packaging/windows/lapdog.nsi
	@echo "installer: $(SETUP)"

# Both the executable and the installer are signed, when there is a certificate.
#
# Signing the inner binary matters because someone running it from the portable zip
# never sees the installer's signature.
#
# The whole recipe is one shell command, joined with backslashes, and that is
# load-bearing rather than style. Make runs each recipe line in a separate shell,
# so the `exit 0` in the no-certificate branch used to end only its own line —
# make then ran the next one, which requires osslsigncode, and the target failed on
# any machine without a certificate. That is the opposite of what is wanted: a
# missing certificate must leave the artefacts unsigned, not break the release.
sign:
	@if [ -z "$(SIGN_PKCS12)" ]; then \
	  echo "sign: SIGN_PKCS12 is not set; leaving artefacts unsigned."; \
	  echo "      Windows shows a SmartScreen warning the first time an unsigned"; \
	  echo "      executable is run. Publish SHA256SUMS so it can be checked."; \
	  exit 0; \
	fi; \
	command -v osslsigncode >/dev/null || { \
	  echo "sign: osslsigncode not found; run 'make tools'"; exit 1; }; \
	for f in $(EXE) $(SETUP); do \
	  [ -f "$$f" ] || continue; \
	  osslsigncode sign -pkcs12 "$(SIGN_PKCS12)" -pass "$(SIGN_PASSWORD)" \
	    -n "LapDog" -i "https://github.com/blezek/lapdog" \
	    -ts "$(TIMESTAMP_URL)" -in "$$f" -out "$$f.signed" && mv "$$f.signed" "$$f"; \
	done; \
	echo "sign: signed $(EXE) $(SETUP)"

release: test-ci vet build-windows portable installer sign
	cd $(DIST) && shasum -a 256 lapdog.exe lapdogctl.exe $(notdir $(PORTABLE)) $(notdir $(SETUP)) > SHA256SUMS
	@echo
	@echo "Release artefacts in $(DIST):"
	@cd $(DIST) && ls -lh lapdog.exe lapdogctl.exe $(notdir $(PORTABLE)) $(notdir $(SETUP)) SHA256SUMS

# Proves the interface really is inside a Windows executable rather than read from
# disk at runtime, by finding strings that only exist in the bundle and icon set.
#
# It checks both shipped binaries, which are the artefacts users receive.
#
# The target is asserted rather than assumed. Greping for strings says nothing
# about what the binary was compiled for, and it passed happily on a darwin build
# that merely had a PE header attached. Go records the real values in the binary,
# so they are read back out of it.
verify-embed: build-windows build-windows-ctl
	@for f in $(EXE) $(CTLEXE); do \
	  go version -m $$f | grep -q "GOOS=windows" || { \
	    echo "verify-embed: $$f was not built for Windows:"; go version -m $$f | grep GOOS; exit 1; }; \
	  go version -m $$f | grep -q "GOARCH=amd64" || { \
	    echo "verify-embed: $$f is not amd64, the shipped target:"; go version -m $$f | grep GOARCH; exit 1; }; \
	  for n in LapDog mdi-racing-helmet; do \
	    grep -qa "$$n" $$f || { echo "verify-embed: $$n is missing from $$f"; exit 1; }; \
	  done; \
	  echo "verify-embed: $$f is windows/amd64 with the interface and icons inside"; \
	done

# Mirrors .github/workflows/ci.yml so the same checks can be run before pushing.
ci: fmt-check vet test-ci verify-embed
	cd web && npm run typecheck && npm run test
	GOOS=windows GOARCH=amd64 go build ./...
	@echo "ci: ok"

clean:
	rm -rf dist .dataset internal/web/dist/assets internal/web/dist/index.html
