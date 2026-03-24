BINARY := bin/colossusx
GO ?= go

.PHONY: help deps build run-help bench-small bench-cpu mine-easy clean

help:
	@echo "Available targets:"
	@echo "  make deps        - Download Go module dependencies"
	@echo "  make build       - Build $(BINARY)"
	@echo "  make run-help    - Show CLI help"
	@echo "  make bench-small - Run a small verified unified benchmark"
	@echo "  make bench-cpu   - Run a small verified CPU benchmark"
	@echo "  make mine-easy   - Run an easy-target mining check"
	@echo "  make clean       - Remove built artifacts"

deps:
	$(GO) mod download

build:
	mkdir -p bin
	$(GO) build -o $(BINARY) .

run-help:
	$(GO) run . -h

bench-small:
	$(GO) run . -bench -backend unified -dag-mib 1 -max-nonces 1000 -workers 2

bench-cpu:
	$(GO) run . -bench -backend cpu -dag-mib 1 -max-nonces 1000 -workers 2

mine-easy:
	$(GO) run . -backend unified -dag-mib 1 -workers 2 -max-nonces 10 -target ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff

clean:
	python -c "import os, shutil; shutil.rmtree('bin', ignore_errors=True)"
