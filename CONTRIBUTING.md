# Contributing to cv

## Development setup

```
git clone https://github.com/marcelocantos/cv.git
cd cv
go build ./...
go test ./...
```

Requires Go 1.25+. cv has zero external dependencies.

## Running tests

```
go test ./...           # all tests
go test -race ./...     # with race detector
go test -v -run TestFoo # single test
```

## Submitting changes

1. Fork the repo and create a feature branch from `master`.
2. Add tests for new functionality.
3. Ensure `go test ./...` and `go vet ./...` pass.
4. Open a pull request against `master`.

Keep commits focused — one logical change per commit.

## Reporting bugs

Open an issue at https://github.com/marcelocantos/cv/issues with:
- What you did (cvfile content, command run)
- What you expected
- What happened instead

## License

By contributing, you agree that your contributions will be licensed
under the Apache 2.0 License.
