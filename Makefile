# A release tag is `vMAJOR.MINOR.PATCH`, while GoReleaser, NSIS version fields,
# and artifact filenames use the numeric part. Use the nearest reachable release
# tag so a release build from a branch descended from that tag does not silently
# fall back to the obsolete development version. Callers can still set VERSION
# explicitly for snapshots, a forthcoming release, and GoReleaser hooks.
GIT_TAG ?= $(shell git describe --tags --abbrev=0 --match 'v[0-9]*.[0-9]*.[0-9]*' 2>/dev/null)
VERSION ?= $(if $(GIT_TAG),$(patsubst v%,%,$(GIT_TAG)),0.1.0)
# Local packages carry enough Git identity to distinguish untagged builds. Keep
# their descriptive version separate from Windows' four-number resource version:
# NSIS can display `0.4.0-3-gabc1234-dirty`, but VIProductVersion cannot encode
# that text. The commit distance is a useful, monotonic fourth component until
# the next release tag resets it to zero.
GIT_LONG ?= $(shell git describe --tags --long --dirty --always --match 'v[0-9]*.[0-9]*.[0-9]*' 2>/dev/null)
LOCAL_VERSION ?= $(if $(filter v%,$(GIT_LONG)),$(patsubst v%,%,$(GIT_LONG)),0.0.0-0-g$(GIT_LONG))
LOCAL_VERSION_PARTS := $(subst -, ,$(LOCAL_VERSION))
LOCAL_WINDOWS_VERSION ?= $(word 1,$(LOCAL_VERSION_PARTS)).$(word 2,$(LOCAL_VERSION_PARTS))
WINDOWS_VERSION ?= $(VERSION).0
REVISION ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
MODULE  := github.com/blezek/lapdog
LDFLAGS := -X $(MODULE)/internal/version.Version=$(VERSION) -X $(MODULE)/internal/version.Revision=$(REVISION) -s -w

# CGO_ENABLED=0 is load-bearing, not a preference. It is why modernc.org/sqlite
# was chosen over the C bindings, and it is what allows the Windows binary to be
# cross-compiled from macOS with no mingw toolchain.
export CGO_ENABLED=0

DIST    := dist
BUNDLE  := internal/web/dist/index.html

# Keep both kinds of Go test concurrency explicit: -p controls package test
# binaries, while -parallel controls tests that call t.Parallel within one binary.
# Otherwise either can silently fall back to a serial caller environment.
GO_TEST_PARALLEL := 4

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
# same target: make run DEV_DB=~/.local/share/lapdog/lapdog.db
DEV_DB   ?= .dataset.db
DEV_PORT ?= 47047

# `make brake-it` writes aggregate Garage61 scenarios into the same SQLite file
# served by `make run`. The JSON is a local, screened staging artifact under the
# already-gitignored ignore/ tree. It keeps Garage61 lap citations for the local
# UI, but contains no token, driver identity, API URL, or raw telemetry.
BRAKE_IT_DB          ?= $(DEV_DB)
BRAKE_IT_CATALOG     ?= ignore/brake-it/catalog.json
BRAKE_IT_EMBEDDED_CATALOG := internal/store/brake_catalog_data/catalog.json
BRAKE_IT_CSV_LIMIT   ?= 12
BRAKE_IT_MAX_LAPS    ?= 100
BRAKE_IT_REQUEST_INTERVAL ?= 1.0
BRAKE_IT_RETRY_JITTER ?= 1.0

# Where `make ingest` reads captures from. Overridable so any directory of .lpd
# files can be replayed: make ingest CAPTURES=path/to/captures
CAPTURES ?= ignore/captures

SIGN_PKCS12   ?=
SIGN_PASSWORD ?=
TIMESTAMP_URL ?= http://timestamp.digicert.com

# Recipes run one at a time.
#
# Several targets write dist/lapdog.exe — portable and installer both need it — and
# under -j two of them can be in that file at once, producing an executable that is
# neither build. Nothing here loses anything by being serial: go build, go test and
# vite all parallelise internally, so make-level concurrency was buying almost
# nothing to begin with.
#
# Global rather than per-target because this is GNU Make 3.81; .NOTPARALLEL took
# target arguments only from 4.4.
.NOTPARALLEL:

.PHONY: help build local-packages ci test run ui-dev dataset dataset-db ingest brake-it release tools clean \
        lint ui verify-embed build-windows build-ctl build-gen \
        fixtures validate portable installer sign goreleaser-check \
		release-snapshot print-version print-local-version

# Only the targets worth typing. The rest are prerequisites of these — real
# targets, still invocable, just not things anyone reaches for directly.
help:
	@echo "build       every check, then every artefact: binaries, zip, installer"
	@echo "local-packages  build zip and installer with a descriptive Git version"
	@echo "ci          every check, and nothing else: what CI runs"
	@echo "test        the Go and web test suites"
	@echo "run         serve $(DEV_DB) on http://127.0.0.1:$(DEV_PORT)"
	@echo "ui-dev      Vite dev server with hot reload, against a running API"
	@echo "dataset     generate the synthetic capture files (~250 MB, gitignored)"
	@echo "dataset-db  replay those captures into $(DEV_DB)"
	@echo "ingest      replay existing captures from $(CAPTURES) into $(DEV_DB)"
	@echo "brake-it    import free-road Garage61 brake scenarios into $(BRAKE_IT_DB)"
	@echo "release     build, then Authenticode-sign and write SHA256SUMS"
	@echo "release-snapshot  local GoReleaser release without publishing"
	@echo "goreleaser-check  validate .goreleaser.yaml"
	@echo "tools       install the macOS packaging toolchain via brew"
	@echo "clean       remove build output and the generated bundle"
	@echo
	@echo "Plumbing, used as prerequisites: lint ui verify-embed portable"
	@echo "installer sign validate fixtures build-windows build-ctl build-gen"

# The bundle-dependent tests skip when the frontend has not been built. That skip
# used to be reachable from `make test`, which meant the quick target silently
# proved less than the CI one — the difference was a whole second target to
# remember. The bundle is a file dependency, so requiring it costs nothing when it
# is already current, and LAPDOG_REQUIRE_BUNDLE turns a skip into a failure.
test: $(BUNDLE)
	LAPDOG_REQUIRE_BUNDLE=1 go test -p=$(GO_TEST_PARALLEL) -parallel=$(GO_TEST_PARALLEL) ./...
	cd web && npm run test
	PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tools/brake-it -p '*_test.py'
	PYTHONDONTWRITEBYTECODE=1 python3 tools/release_version_test.py

# Used by the release-version test and useful when auditing an artifact build.
print-version:
	@echo "$(VERSION)"

print-local-version:
	@echo "$(LOCAL_VERSION) $(LOCAL_WINDOWS_VERSION)"

# gofmt -l lists unformatted files and exits 0 regardless, so the failure has to
# come from the output being non-empty rather than from the exit code. This is
# what would have caught the unformatted files that reached main unnoticed.
lint:
	go vet ./...
	@files="$$(gofmt -l ./internal ./cmd)"; \
	if [ -n "$$files" ]; then \
	  echo "lint: these files are not gofmt-formatted:"; \
	  echo "$$files"; \
	  exit 1; \
	fi

# The frontend bundle is generated, not committed: it is about a megabyte of
# minified JavaScript that changes wholesale on every UI edit, so committing it
# would bury real changes under regenerated noise. CI builds it, and so does any
# target that needs it — see BUNDLE below.
# To force a rebuild when the inputs look unchanged: make clean ui
ui: $(BUNDLE)

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
# Both shipped Windows binaries, because they always ship together and splitting
# them was two targets describing one release.
#
# The -H windowsgui on the first and its absence on the second is the load-bearing
# difference: the tray app must have no console window, and lapdogctl must have one
# or it has nowhere to print.
#
# lapdogctl.exe ships because the machine that needs diagnosing is a Windows machine
# with a simulator and no development environment. `lapdogctl inspect` on a capture
# is how a telemetry problem gets identified there, and requiring a Go toolchain to
# obtain it puts the tool out of reach exactly when it is needed.
build-windows: $(BUNDLE)
	GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui $(LDFLAGS)" -o $(EXE) ./cmd/lapdog
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(CTLEXE) ./cmd/lapdogctl

# The host build of the CLI, for development on this machine.
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

# Replay an existing directory of captures into the development database.
#
# Unlike `make dataset-db`, which replays the generated synthetic set, this ingests
# whatever real captures already exist — the ones the tray application recorded from
# a live simulator, kept under ignore/captures. It is the same ingest path, so the
# resulting database has been through the same decode, classify and accounting code
# as a live session. The glob is non-recursive: only *.lpd directly in CAPTURES are
# read, and ingest itself fails if it finds none.
ingest: build-ctl
	@test -d $(CAPTURES) || { \
	  echo "ingest: $(CAPTURES) does not exist; set CAPTURES=path/to/captures"; exit 1; }
	rm -f $(DEV_DB) $(DEV_DB)-wal $(DEV_DB)-shm
	./dist/lapdogctl ingest $(CAPTURES) $(DEV_DB)
	./dist/lapdogctl summary $(DEV_DB)

# Build aggregate Brake-It targets for the configured iRacing cars and road
# tracks. The active tracks include both reviewed membership content and
# explicitly requested paid venues. The dedicated token remains in this process
# environment. Raw CSV and lap-level metadata are reduced in memory and are
# never written to the repository or DB.
brake-it:
	@test -n "$${GARAGE61_TOKEN:-}" || { \
	  echo "brake-it: GARAGE61_TOKEN is required"; \
	  echo "          export GARAGE61_TOKEN=... and retry"; exit 2; }
	mkdir -p $(dir $(BRAKE_IT_CATALOG))
	GARAGE61_REQUEST_INTERVAL_SECONDS=$(BRAKE_IT_REQUEST_INTERVAL) \
	GARAGE61_RETRY_JITTER_SECONDS=$(BRAKE_IT_RETRY_JITTER) \
	python3 tools/brake-it/garage61_catalog.py \
	  --output $(BRAKE_IT_CATALOG) \
	  --csv-limit $(BRAKE_IT_CSV_LIMIT) \
	  --max-laps $(BRAKE_IT_MAX_LAPS)
	mkdir -p $(dir $(BRAKE_IT_EMBEDDED_CATALOG))
	cp $(BRAKE_IT_CATALOG) $(BRAKE_IT_EMBEDDED_CATALOG)
	$(MAKE) build-ctl
	./dist/lapdogctl import-brake-catalog $(BRAKE_IT_CATALOG) $(BRAKE_IT_DB)

# Serve a database locally, for looking at the interface with real data in it.
#
# This is the quickest way to see the UI: the synthetic dataset covers two years of
# sessions, so every chart has something in it. The tray application is the other
# way to run the interface, but on a development machine it has no simulator to read
# and so shows an empty database.
run: build-ctl
	@test -f $(DEV_DB) || { \
	  echo "run: $(DEV_DB) does not exist. To create it:"; \
	  echo "       make dataset      # generate captures (~250 MB, a few minutes)"; \
	  echo "       make dataset-db   # replay them into $(DEV_DB)"; \
	  echo "     Or point at another database: make run DEV_DB=path/to.db"; exit 1; }
	@echo "run: http://127.0.0.1:$(DEV_PORT)  (Ctrl-C to stop)"
	./dist/lapdogctl serve $(DEV_DB) $(DEV_PORT)

# The whole Windows toolchain runs on macOS, which is why the release needs no
# Windows machine. See the packaging spec, section 4.1.
tools:
	brew install goreleaser makensis msitools osslsigncode

# Both executables are genuinely self-contained, so a zip is a legitimate
# distribution channel rather than a degraded one.
#
# The archive is removed first rather than written into. zip adds to an existing
# archive and replaces only the entries it is given, so a rebuild after renaming or
# dropping a file would leave the old entry in place — a stale binary shipping
# beside a current one, with nothing to indicate it.
portable: build-windows
	rm -f $(PORTABLE)
	cd $(DIST) && zip -q -9 $(notdir $(PORTABLE)) lapdog.exe lapdogctl.exe
	@echo "portable: $(PORTABLE)"

installer: build-windows
	@command -v makensis >/dev/null || { echo "makensis not found; run 'make tools'"; exit 1; }
	makensis -NOCD -V2 \
	  -DVERSION=$(VERSION) \
	  -DNUMERIC_VERSION=$(WINDOWS_VERSION) \
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

# Everything: every check, then every artefact.
#
# Distinct from ci, which proves the tree is sound but leaves nothing to keep.
# Distinct from release, which additionally signs and writes SHA256SUMS — release
# cuts a version, build just builds one.
#
# ci comes first so a failing test stops the run before anything is packaged. A
# tree that does not pass has no business producing an installer.
build: ci build-ctl build-gen portable installer
	@echo
	@echo "Artefacts in $(DIST):"
	@cd $(DIST) && ls -lh lapdog.exe lapdogctl.exe lapdogctl lapdog-gen \
	  $(notdir $(PORTABLE)) $(notdir $(SETUP))

# Development packages are deliberately opt-in and local. Verify first under the
# normal release-version rules: command-line make variables propagate through
# MAKEFLAGS, so passing the descriptive VERSION to `make build` would contaminate
# the release-version tests. Only the artifact sub-make receives the local values.
# VERSION remains visible in the UI and filenames; WINDOWS_VERSION satisfies the
# numeric version resource embedded by NSIS.
local-packages: ci
	$(MAKE) build-ctl build-gen portable installer \
	  VERSION="$(LOCAL_VERSION)" WINDOWS_VERSION="$(LOCAL_WINDOWS_VERSION)"
	@echo
	@echo "Local packages in $(DIST):"
	@cd $(DIST) && ls -lh lapdog.exe lapdogctl.exe lapdogctl lapdog-gen \
	  "lapdog-$(LOCAL_VERSION)-portable.zip" "lapdog-$(LOCAL_VERSION)-setup.exe"
	-rsync -av $(DIST)/*setup* "$${HOME}/Dropbox/Lapdog/"

release: build sign
	cd $(DIST) && shasum -a 256 lapdog.exe lapdogctl.exe $(notdir $(PORTABLE)) $(notdir $(SETUP)) > SHA256SUMS
	@echo
	@echo "Release artefacts in $(DIST):"
	@cd $(DIST) && ls -lh lapdog.exe lapdogctl.exe $(notdir $(PORTABLE)) $(notdir $(SETUP)) SHA256SUMS

goreleaser-check:
	goreleaser check

release-snapshot:
	goreleaser release --snapshot --clean --skip=publish

# Proves the interface really is inside a Windows executable rather than read from
# disk at runtime, by finding strings that only exist in the bundle and icon set.
#
# It checks both shipped binaries, which are the artefacts users receive.
#
# The target is asserted rather than assumed. Greping for strings says nothing
# about what the binary was compiled for, and it passed happily on a darwin build
# that merely had a PE header attached. Go records the real values in the binary,
# so they are read back out of it.
verify-embed: build-windows
	@for f in $(EXE) $(CTLEXE); do \
	  go version -m $$f | grep -q "GOOS=windows" || { \
	    echo "verify-embed: $$f was not built for Windows:"; go version -m $$f | grep GOOS; exit 1; }; \
	  go version -m $$f | grep -q "GOARCH=amd64" || { \
	    echo "verify-embed: $$f is not amd64, the shipped target:"; go version -m $$f | grep GOARCH; exit 1; }; \
	  for n in LapDog Brake-It mdi-racing-helmet; do \
	    grep -qa "$$n" $$f || { echo "verify-embed: $$n is missing from $$f"; exit 1; }; \
	  done; \
	  echo "verify-embed: $$f is windows/amd64 with the interface and icons inside"; \
	done

# Mirrors .github/workflows/ci.yml so the same checks can be run before pushing.
ci: lint test verify-embed
	cd web && npm run typecheck
	GOOS=windows GOARCH=amd64 go build ./...
	@echo "ci: ok"

clean:
	rm -rf dist .dataset internal/web/dist/assets internal/web/dist/index.html
