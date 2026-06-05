# golinkname.el

An Emacs Lisp package (`golinkname.el`), which wraps the
[golinkname](https://github.com/dnaeon/golinkname) CLI for navigating through
`//go:linkname` directives in a Go codebase.

# Install the binary

`golinkname.el` shells out to the `golinkname` binary, so install that
first and make sure it is on `exec-path`:

```sh
go install -v github.com/dnaeon/golinkname/cmd/golinkname@latest
```

# Install the package

The package is a single file with no third-party dependencies. Drop it into your
`load-path` however you usually do:

## `use-package` + `straight`

```elisp
(use-package golinkname
  :straight (:host github :repo "dnaeon/golinkname"
             :files ("editor/emacs/golinkname.el"))
  :commands (golinkname-find-references
             golinkname-list
             golinkname-list-buffer
             golinkname-diagnose))
```

## Plain `load-path`

```elisp
(add-to-list 'load-path "/path/to/golinkname/editor/emacs")
(require 'golinkname)
```

# Interactive commands

All commands are namespaced `golinkname-*` and discoverable through `M-x`.

| Command                      | What it does                                                                              |
|------------------------------|-------------------------------------------------------------------------------------------|
| `golinkname-find-references` | Show every linkname directive in the current module related to the symbol at point        |
| `golinkname-list`            | Tabulated view of every directive in the current module.                                  |
| `golinkname-list-buffer`     | Tabulated view of every directive in the current buffer.                                  |
| `golinkname-diagnose`        | Print a status report (binary path, project root, last invocation result). For debugging. |

# Customization

```elisp
;; Path to the golinkname binary (default: "golinkname" -- resolved on exec-path).
(setq golinkname-executable "/usr/local/bin/golinkname")

;; Extra args appended to every invocation.  Useful when you want to
;; pin the project root rather than relying on auto-detection.
(setq golinkname-extra-args '("--dir" "/abs/path/to/module"))
```

# Tests

```sh
cd editor/emacs
emacs -Q --batch -L . -l golinkname-tests.el -f ert-run-tests-batch-and-exit
```

The end-to-end tests build `golinkname` on demand (skipped if `go` is not on
`PATH`); to use a pre-built binary, set `GOLINKNAME_BIN`:

```sh
GOLINKNAME_BIN=$(pwd)/../../bin/golinkname \
  emacs -Q --batch -L . -l golinkname-tests.el -f ert-run-tests-batch-and-exit
```
