[![CI](https://github.com/go-shiori/go-epub/workflows/CI/badge.svg)](https://github.com/go-shiori/go-epub/actions)
[![Coverage Status](https://coveralls.io/repos/github/go-shiori/go-epub/badge.svg)](https://coveralls.io/github/go-shiori/go-epub)
[![Go Report Card](https://goreportcard.com/badge/github.com/go-shiori/go-epub)](https://goreportcard.com/report/github.com/go-shiori/go-epub)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/go-shiori/go-epub/blob/master/LICENSE)
[![GoDoc](https://godoc.org/github.com/go-shiori/go-epub?status.svg)](https://godoc.org/github.com/go-shiori/go-epub)

---

### Features
- [Documented API](https://godoc.org/github.com/go-shiori/go-epub)
- Creates valid EPUB 3.0 files
- Adds an additional EPUB 2.0 table of contents for maximum compatibility
- Includes support for adding CSS, images, and fonts

### Contributions

Contributions are welcome; please see [CONTRIBUTING.md](CONTRIBUTING.md) for more information.

### Development

Clone this repository using Git. Run tests as documented below.

Dependencies are managed using [Go modules](https://golang.org/ref/mod)

### Testing

#### EPUBCheck

EPUBCheck is a tool that will check an EPUB for validation errors.

If EPUBCheck is installed locally, it will be run alongside the Go tests. To install EPUBCheck:

1. Make sure you have Java installed on your system

1. Get the latest version of EPUBCheck from [https://github.com/w3c/epubcheck/releases](https://github.com/w3c/epubcheck/releases)

1. Download and extract EPUBCheck in the root directory of this project, e.g.

   ```
   wget https://github.com/IDPF/epubcheck/releases/download/v5.1.0/epubcheck-5.1.0.zip
   unzip epubcheck-5.1.0.zip
   ```

   You can use this command to download and extract the latest versions of EPUBCheck (recommended).

   ```
   curl -s https://api.github.com/repos/w3c/epubcheck/releases/latest | awk -F': "' '/browser_download_url/ && /epubcheck/ {gsub(/"$/, "", $2); print $2}' | xargs curl -Lo epubcheck.zip
   unzip epubcheck.zip
   ```

If you do not wish to install EPUBCheck locally, you can manually validate the EPUB:

1. Set `doCleanup = false` in epub_test.go

1. Run the tests (see below)

1. Upload the generated `My EPUB.epub` file to [http://validator.idpf.org/](http://validator.idpf.org/)

#### Run tests

```
go test
```
