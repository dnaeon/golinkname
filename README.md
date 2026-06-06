# golinkname

A tool for indexing and navigating through `//go:linkname` compiler directives
in Go modules.

The [//go:linkname directive](https://pkg.go.dev/cmd/compile#hdr-Compiler_Directives))
creates a linker-level alias between two object-file symbols, letting one
package reach an unexported declaration in another package.

It is used heavily in the standard library and in low-level Go code, but tooling
support for it is partial (at least to my knowledge).

For instance `gopls` (since v0.12) [supports goto-definition and hover](https://github.com/golang/go/issues/57312)
on the _two-argument_ form only.

However `gopls` does not support finding references in the reverse
direction. For a given target symbol, we cannot really tell which
`//go:linkname` directive _pulls_ from it, which means you have to resort to
`grep(1)`'ing your way out in order to view the relations between the symbols.

`golinkname` attempts to fill these gaps. It walks a Go module, parses every
`//go:linkname` directive (both forms), source-resolves in-module targets via
`go/parser`, and emits a stable JSON suitable for editor and LSP integration.

# Requirements

- [GNU Make](https://www.gnu.org/software/make/)
- [Go](https://go.dev) version 1.26.x or later

# Installation

You can install `golinkname` using the following command.

``` shell
go install -v github.com/dnaeon/golinkname/cmd/golinkname@latest
```

Or you can build a binary locally using the following command instead.

``` shell
make build
```

# CLI

The following command will list all `//go:linkname` references in the current Go
module.

``` shell
golinkname list
```

You can also list the `linkname` directives from a module located elsewhere by
using the `--dir|-C` option. The following command lists all `linkname`
directives and their associated symbols from the standard library.

``` shell
$ golinkname list --dir "$( go env GOROOT)/src"
FILE:LINE                                           FORM                 DIR   LOCAL                                          TARGET                                              RESOLVED                                            WARNINGS
arena/arena.go:87                                   func/two-arg         push  reflect_arena_New                              reflect.arena_New                                   reflect/arena.go:18                                 -
arena/arena.go:92                                   func/one-arg         pull  runtime_arena_newArena                         -                                                   -                                                   -
arena/arena.go:95                                   func/one-arg         pull  runtime_arena_arena_New                        -                                                   -                                                   -
arena/arena.go:101                                  func/one-arg         pull  runtime_arena_arena_Slice                      -                                                   -                                                   -
arena/arena.go:104                                  func/one-arg         pull  runtime_arena_arena_Free                       -                                                   -                                                   -
arena/arena.go:107                                  func/one-arg         pull  runtime_arena_heapify                          -                                                   -                                                   -
crypto/fips140/enforcement.go:40                    func/one-arg         pull  setBypass                                      -                                                   -                                                   -
crypto/fips140/enforcement.go:43                    func/one-arg         pull  isBypassed                                     -                                                   -                                                   -
crypto/fips140/enforcement.go:46                    func/one-arg         pull  unsetBypass                                    -                                                   -                                                   -
crypto/internal/fips140/cast.go:16                  func/two-arg         pull  fatal                                          crypto/internal/fips140.fatal                       crypto/internal/fips140/cast.go:17                  -
crypto/internal/fips140/check/check.go:32           var/two-arg-extern   pull  Linkinfo                                       go:fipsinfo                                         -                                                   -
crypto/internal/fips140/check/checktest/test.go:20  var/two-arg          pull  RODATA                                         crypto/internal/fips140/check/checktest.RODATA      crypto/internal/fips140/check/checktest/test.go:21  -

...
```

The following command builds and returns an _index_ of the discovered `linkname`
directives in JSON format.

``` shell
golinkname index --pretty --dir /path/to/some/module
```

The result can be used to integrate with other tools, editors or IDEs.

In order to find who references a given symbol via a `linkname` directive we can
use the following command.

``` shell
golinkname refs my-module/pkg/foo.fooVar
```

This command tells you the symbols to which a given symbol is _related_, e.g. it
references some other symbols via a `linkname` directive.

``` shell
golinkname related my-module/pkg/bar.something
```

# Library

The CLI is a thin wrapper over the `pkg/linkname` package. You can also use it
as a library in your project.

``` go
import "github.com/dnaeon/golinkname/pkg/linkname"

records, err := linkname.Index(".")
```

# Docker

You can build a Docker image with the `golinkname` tool.

``` shell
# Build the image locally.
docker build -t golinkname:latest .

# Run against a module on the host. Mount the module read-only at /src
# and pass `-C /src` so the tool runs against it.
docker run --rm -v "$PWD:/src:ro" golinkname:latest index -C /src --pretty
```

# Emacs

An Emacs Lisp package, which wraps the `golinkname` tool can be found in the
[editor/emacs](./editor/emacs) directory of this repo. Check its documentation
for more details on how to install and use it.

# License

`golinkname` is distributed under the BSD-3-Clause license. See [LICENSE](LICENSE).

# Credits

Portions of `pkg/linkname/parse.go` are derived from
[gopls/internal/golang/linkname.go](https://github.com/golang/tools/blob/master/gopls/internal/golang/linkname.go)
in `golang.org/x/tools`.

The original code is copyright of The Go Authors and is distributed under the
3-clause BSD license; the upstream license header is preserved verbatim at the
top of that file, and the full upstream license text is reproduced in
[`LICENSE-third-party`](LICENSE-third-party).
