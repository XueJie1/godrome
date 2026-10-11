GO ?= go
export CGO_ENABLED := 1

.PHONY: run build check test test-audio samples measure acceptance-build
run:
	$(GO) run -buildvcs=false .
build:
	mkdir -p build
	$(GO) build -buildvcs=false -trimpath -ldflags='-s -w -X github.com/egoist/mygo.production=1' -o build/godrome .
check:
	test -z "$$($(GO) fmt ./...)"
	$(GO) vet ./...
test:
	$(GO) test -race ./...
samples:
	sh scripts/audio-samples.sh
test-audio: samples
	GODROME_AUDIO_SAMPLES="$(CURDIR)/testdata/audio" $(GO) test -race ./internal/player -v
measure: build
	python3 scripts/measure.py --output docs/evidence/memory/empty -- ./build/godrome --empty
acceptance-build:
	mkdir -p build
	$(GO) build -buildvcs=false -trimpath -ldflags='-s -w -X github.com/egoist/mygo.production=1' -o build/godrome-acceptance app.go metrics.go acceptance_main.go
