_default:
    @just --list --unsorted

# Run the app from source; extra flags pass through (e.g. just run --sessions)
run *args:
    go run . {{args}}

# Run all tests; extra flags pass through (e.g. just test -v)
test *args:
    go test {{args}} ./...

# Verify formatting and run static analysis
check:
    test -z "$(gofmt -l .)"
    go vet ./...
    golangci-lint run

# Build and install the binary into GOBIN
install:
    go install .

# Rebuild docs/demo.gif from a synthetic fixture (requires vhs)
screenshot:
    vhs docs/demo.tape
