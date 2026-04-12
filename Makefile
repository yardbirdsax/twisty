.PHONY: test test-integration lint

# Note: integration tests are skipped here because -short is set.
# Run `make test-integration` to execute the full test suite including integration tests.
test:
	go test -short ./...

test-integration:
	go test ./...

lint:
	go vet ./...

# --- Build ---

# Keychain key used to retrieve the Google Routes API key at build time.
# Store it once with:
#   security add-generic-password -s twisty/build/google/api-key -a twisty -w '<value>'
GOOGLE_API_KEY ?= $(shell security find-generic-password -s twisty/build/google/api-key -a twisty -w 2>/dev/null)

GOOGLE_LDFLAGS = -X main.builtInGoogleAPIKey=$(GOOGLE_API_KEY)

.PHONY: build install
build: ## Build the twisty binary with the Google API key injected from the macOS keychain.
	@if [ -z "$(GOOGLE_API_KEY)" ]; then \
		echo "ERROR: twisty/build/google/api-key not found in keychain."; \
		echo "  security add-generic-password -s twisty/build/google/api-key -a twisty -w '<value>'"; \
		exit 1; \
	fi
	@mkdir -p bin
	go build -ldflags "$(GOOGLE_LDFLAGS)" -o bin/twisty .

.PHONY: install
install: ## Install the twisty binary to $(GOPATH)/bin with the Google API key injected from the macOS keychain.
	@if [ -z "$(GOOGLE_API_KEY)" ]; then \
		echo "ERROR: twisty/build/google/api-key not found in keychain."; \
		echo "  security add-generic-password -s twisty/build/google/api-key -a twisty -w '<value>'"; \
		exit 1; \
	fi
	go install -ldflags "$(GOOGLE_LDFLAGS)" .

# --- Local Overpass API ---
OVERPASS_ARGS ?=

.PHONY: overpass-start overpass-stop overpass-status overpass-clean overpass-logs

overpass-start:
	go run . overpass start $(OVERPASS_ARGS)

overpass-stop:
	go run . overpass stop

overpass-status:
	go run . overpass status

overpass-clean:
	go run . overpass clean

overpass-logs:
	go run . overpass logs

# --- Benchmarks ---
BENCHMARK_PBF    ?= .benchmark-data/north-america_us_pennsylvania-latest.osm.pbf
BENCHMARK_OUTPUT ?= /tmp/benchmark-output.osm.bz2

.PHONY: benchmark-setup benchmark-go benchmark-osmium benchmark

benchmark-setup: $(BENCHMARK_PBF)

$(BENCHMARK_PBF):
	mkdir -p .benchmark-data
	curl -L -o $@ https://download.geofabrik.de/north-america/us/pennsylvania-latest.osm.pbf

benchmark-go: $(BENCHMARK_PBF)
	BENCHMARK_PBF=$(abspath $(BENCHMARK_PBF)) BENCHMARK_OUTPUT=$(BENCHMARK_OUTPUT) \
	  go test -bench=BenchmarkConvert -benchtime=1x -v ./osmconv/

benchmark-osmium: $(BENCHMARK_PBF)
	@echo "Input:  $$(du -h $(BENCHMARK_PBF) | cut -f1) $(BENCHMARK_PBF)"
	/usr/bin/time -p docker run --rm \
	  -v $(abspath $(BENCHMARK_PBF)):/data/input.osm.pbf \
	  -v $(dir $(abspath $(BENCHMARK_OUTPUT))):/out \
	  wiktorn/overpass-api \
	  osmium cat /data/input.osm.pbf -o /out/$(notdir $(BENCHMARK_OUTPUT)) --overwrite
	@echo "Output: $$(du -h $(BENCHMARK_OUTPUT) | cut -f1) $(BENCHMARK_OUTPUT)"

benchmark: benchmark-go benchmark-osmium
