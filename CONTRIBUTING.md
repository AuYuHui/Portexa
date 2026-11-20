# Contributing to Portexa

[English README](README.md) · [中文说明](README.zh-CN.md) · [Documentation](docs/README.md)

Issues and pull requests are welcome. Explain the problem or intended behavior, include a small synthetic reproduction where possible, and state what you verified.

## Local setup

Use macOS or Linux with Go 1.26+, Node.js 22.12+, npm, and Python 3. Start with the [quick start](README.md#quick-start), then run:

```sh
make check
```

The check runs Go tests and `go vet` with `GOWORK=off`, Python tests for capture tooling and MT5 handshake research, frontend type and translation checks, the web build, and structure checks. Browser acceptance and screenshot capture run separately. [Acceptance records](docs/phase1-validation.md) · [Screenshot generation](docs/screenshots.md)

## Code and tests

- Keep MT4 protocol code in `packages/mt4` and MT5 protocol code in `packages/mt5`. Both SDKs expose typed protocol data and run independently of the main application, storage, CLI input/output, and research tools.
- Keep application behavior in `internal/` and command entry points in `cmd/`. `internal/cli` handles probe input and redacted output; `internal/appcli` handles product administration.
- Put SDK tests in `packages/mt4/tests` and `packages/mt5/tests`, application and capture-tool tests in root `tests`, and historical experiment tests in the relevant `research/*/tests` directory.
- Keep source, test, script, and documentation files at or below 1,000 lines. `make structure` checks the limit and module boundaries.
- Use `gofmt` for changed Go files. Update related documentation when behavior, configuration, or supported scope changes.

Use synthetic data and loopback servers for ordinary regression checks. Live broker connections are separate, explicitly authorized work; a passing synthetic test is not evidence of live compatibility.

## Translations

The UI uses Vue I18n, with messages in `web/src/locales/en.json` and `web/src/locales/zh-CN.json`. Keep message keys and interpolation parameters aligned:

```sh
npm --prefix web run i18n:check
npm --prefix web run i18n:ui
```

Adding a language requires registering it in `web/src/i18n.ts`, the language selector, and `web/x18n.config.json`. Preserve account names, raw order comments, money precision, and the meaning of server wall-clock times.

Maintain both root READMEs when changing public setup instructions or feature descriptions. Regenerate the desktop screenshots when the displayed behavior changes.

## Private data

Keep real credentials, endpoints, device material, packet captures, and account details in ignored local directories. Use reserved example addresses and synthetic records in source, tests, screenshots, and public issues. Never put passwords in command-line arguments or commit private configuration.

## Pull requests

Describe the problem or requirement, the implementation approach, relevant checks, and any unverified behavior. Use a Conventional Commit title such as `docs:`, `feat:`, or `fix:`; repository commit descriptions use Chinese. Keep unrelated changes out of the patch.

## License

Contributions are made under the repository's [MIT License](LICENSE). Preserve third-party copyright and license notices when incorporating or redistributing dependencies.
