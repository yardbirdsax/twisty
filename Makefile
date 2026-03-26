.PHONY: test test-integration lint

# Note: integration tests are skipped here because -short is set.
# Run `make test-integration` to execute the full test suite including integration tests.
test:
	go test -short ./...

test-integration:
	go test ./...

lint:
	go vet ./...

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
