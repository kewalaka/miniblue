# Contributing to miniblue

Read [CODING_STANDARDS.md](CODING_STANDARDS.md) before writing code and during code review.

## Getting started

1. Fork the repository.
2. Clone your fork: `git clone https://github.com/YOUR_USERNAME/miniblue.git`
3. Create a branch: `git checkout -b feature/my-feature`
4. Make your changes.
5. Run the build and tests: `go build ./... && go test ./...`
6. Commit and push your branch.
7. Open a pull request.

## Prerequisites for Python examples

On Debian and Ubuntu, install `python3-venv` (or the version-specific package) before creating a virtual environment:

```bash
# Debian/Ubuntu
sudo apt install python3.12-venv   # or python3-venv for the system default

# Then create a venv as normal
python3 -m venv venv
source venv/bin/activate
pip install -r requirements.txt
```

## Adding a new Azure service

Each service is in its own package under `internal/services/`. Follow this pattern:

1. Create `internal/services/yourservice/handler.go`.
2. Define a `Handler` struct with a `*store.Store` field.
3. Implement `NewHandler(s *store.Store) *Handler`.
4. Implement `Register(r chi.Router)` to set up routes.
5. Register your handler in `internal/server/server.go`.

### Example skeleton

```go
package yourservice

import (
    "github.com/go-chi/chi/v5"
    "github.com/moabukar/miniblue/internal/store"
)

type Handler struct {
    store *store.Store
}

func NewHandler(s *store.Store) *Handler {
    return &Handler{store: s}
}

func (h *Handler) Register(r chi.Router) {
    // Add your routes here
}
```

## Code style

Follow [CODING_STANDARDS.md](CODING_STANDARDS.md) before writing code and at review.

- Run `go vet ./...` before committing.
- Use standard Go formatting (`gofmt`).
- Keep handlers focused on one service per package.
- Use regular hyphens (`-`), not em dashes.
- Use British spelling in documentation (colour, behaviour).

## Reporting bugs

Use the bug report template when opening an issue. Include:
- Steps to reproduce
- Expected and actual behaviour
- Your environment (OS, Go version, Docker or binary)

## Suggesting services

Open a feature request issue with the Azure service name and links to the relevant Azure REST API docs. This helps us match the real Azure API.

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
