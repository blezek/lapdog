VERSION ?= 0.1.0
MODULE  := github.com/blezek/lapdog
LDFLAGS := -X $(MODULE)/internal/version.Version=$(VERSION) -s -w

# CGO_ENABLED=0 is load-bearing, not a preference. It is why modernc.org/sqlite
# was chosen over the C bindings, and it is what allows the Windows binary to be
# cross-compiled from macOS with no mingw toolchain.
export CGO_ENABLED=0

.PHONY: help test vet build-windows build-ctl build-gen fixtures dataset validate clean

help:
	@echo "test           run the unit tests"
	@echo "vet            run go vet"
	@echo "build-windows  cross-compile the tray app for Windows"
	@echo "build-ctl      build the lapdogctl development CLI"
	@echo "build-gen      build the dataset generator"
	@echo "fixtures       regenerate the committed test fixtures (~1.7 MB)"
	@echo "dataset        generate the full two-year dataset into .dataset (~250 MB, gitignored)"
	@echo "validate       replay .dataset back through decode, parse and classify"

test:
	go test ./...

vet:
	go vet ./...

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

clean:
	rm -rf dist .dataset
