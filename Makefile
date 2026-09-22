# The same commands CI runs (.github/workflows/ci.yml), so a green `make all`
# means a green pull request. Tool versions are pinned here and in both
# workflows; scripts/doc-check.sh fails if they disagree.

GO       ?= go
FUZZTIME ?= 60s

.PHONY: all lint fmt vet tidy staticcheck vuln actionlint doccheck test race bench fuzz docs profile

all: lint test race bench fuzz docs

## lint: formatting, vet, tidy go.mod, staticcheck, govulncheck, actionlint, doc comments
lint: fmt vet tidy staticcheck vuln actionlint doccheck

fmt:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; echo "gofmt: the files above need formatting"; exit 1; }

vet:
	$(GO) vet ./...

tidy:
	$(GO) mod tidy
	git diff --exit-code go.mod go.sum

staticcheck:
	$(GO) run honnef.co/go/tools/cmd/staticcheck@2026.2.1 ./...

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...

actionlint:
	$(GO) run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12

doccheck:
	scripts/doc-check.sh

## test: unit, acceptance and example tests
test:
	$(GO) test -count=1 ./...

## race: the same under the race detector (the zero-alloc guards skip themselves)
race:
	$(GO) test -count=1 -race ./...

## bench: benchmarks; fails unless the hot paths report 0 allocs/op
bench:
	scripts/check-allocs.sh bench.txt

## fuzz: both fuzz targets for FUZZTIME each (default 60s)
fuzz:
	$(GO) test -run xxx -fuzz 'FuzzAppend$$' -fuzztime $(FUZZTIME) .
	$(GO) test -run xxx -fuzz 'FuzzEach$$' -fuzztime $(FUZZTIME) .

## docs: regenerate the derived pages and check every example in the docs
docs:
	scripts/docs-sync.sh
	$(GO) test -count=1 -run 'TestDocs' .

## profile: CPU, memory, mutex and block profiles of the hot benchmarks into prof/, top 20 of each
# The memory profile samples every allocation so that the one-off state
# builds show; that makes the runtime's own profiling locks contend, so the
# mutex and block profiles come from a second run without it.
profile:
	mkdir -p prof
	for b in BenchmarkAppend BenchmarkRudderEach BenchmarkAppendParallel BenchmarkEachParallel; do \
		$(GO) test -run xxx -bench "^$$b\$$" -benchtime 3s -o prof/$$b.test \
			-cpuprofile prof/$$b.cpu -memprofile prof/$$b.mem -memprofilerate 1 . || exit 1; \
		$(GO) test -run xxx -bench "^$$b\$$" -benchtime 3s \
			-mutexprofile prof/$$b.mutex -blockprofile prof/$$b.block . || exit 1; \
		for p in cpu mem mutex block; do \
			echo; echo "== $$b: $$p"; $(GO) tool pprof -top -nodecount=20 prof/$$b.test prof/$$b.$$p; \
		done; \
	done
