# Contributing to tuntop

Thanks for your interest! The project is in early development, so please
open an issue to discuss larger changes before writing code.

## Development

```bash
make test   # unit tests
make lint   # golangci-lint (includes formatting checks)
make fmt    # apply formatting
make build  # binaries into bin/
```

A change is ready when `make lint test` passes.

## Conventions

- Commits follow [Conventional Commits](https://www.conventionalcommits.org/):
  `feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`. Mark breaking
  changes with `!` and explain them in the body.
- User-visible changes get a line under `[Unreleased]` in
  [CHANGELOG.md](CHANGELOG.md).
- Every Go file starts with `// SPDX-License-Identifier: GPL-3.0-or-later`.
- Never commit real subscription links, server URIs or keys, not even in
  tests. Use obviously fake values (`example.com`, zeroed UUIDs).

## License

By contributing you agree that your contributions are licensed under
GPL-3.0-or-later.
