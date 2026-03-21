.PHONY: test test-integration lint

# Note: integration tests are skipped here because -short is set.
# Run `make test-integration` to execute the full test suite including integration tests.
test:
	go test -short ./...

test-integration:
	go test ./...

lint:
	go vet ./...
