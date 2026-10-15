default: test

# Install dependencies, pre-commit hooks, and the playwright chromium browser.
setup:
    go mod download
    uv sync
    uv run pre-commit install
    uv run playwright install chromium

# Run the Go tests, then the container tests (which need podman).
test: test-go
    uv run pytest -x

test-go:
    go test ./...

# Format, lint, and vet (same checks as the pre-commit hooks).
check:
    gofmt -w cmd internal
    go vet ./...
    uv run ruff check --fix .
    uv run ruff format .

# Move to bottled-bridge-core at ref, for its UI files (a commit, branch or tag; it must be pushed).
update-core ref="main":
    go get github.com/cloud-in-a-bottle/bottled-bridge-core@{{ref}}
    go mod tidy

# Move to the messaging service spec at ref.
update-spec ref="main":
    go get github.com/cloud-in-a-bottle/messaging-service-spec@{{ref}}
    go mod tidy

# Build the container image.
build:
    podman build -t messaging-inbox .
