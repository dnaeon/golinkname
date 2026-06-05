;;; golinkname-tests.el --- Tests for golinkname.el -*- lexical-binding: t; -*-

;; Copyright (c) 2026 Marin Atanasov Nikolov <dnaeon@gmail.com>
;; SPDX-License-Identifier: BSD-3-Clause

;;; Commentary:

;; Run with:
;;
;;   emacs -Q --batch -L . -l golinkname-tests.el -f ert-run-tests-batch-and-exit
;;
;; The end-to-end tests build the `golinkname' binary first; pass an
;; explicit GOLINKNAME_BIN environment variable to skip the build:
;;
;;   GOLINKNAME_BIN=/path/to/golinkname emacs -Q --batch ...

;;; Code:

(require 'ert)
(require 'golinkname)

;;;; Module-path / pkgpath helpers

(ert-deftest golinkname-test-read-module-path ()
  "The `module' line in go.mod is captured."
  (let ((tmp (make-temp-file "golinkname-gomod-")))
    (unwind-protect
        (progn
          (write-region "module example.com/m\n\ngo 1.21\n" nil tmp)
          (should (equal (golinkname--read-module-path tmp)
                         "example.com/m")))
      (delete-file tmp))))

(ert-deftest golinkname-test-read-module-path-quoted ()
  "A quoted module path is captured without the surrounding quotes."
  (let ((tmp (make-temp-file "golinkname-gomod-")))
    (unwind-protect
        (progn
          (write-region "module \"example.com/m\"\n\ngo 1.21\n" nil tmp)
          (should (equal (golinkname--read-module-path tmp)
                         "example.com/m")))
      (delete-file tmp))))

(ert-deftest golinkname-test-read-module-path-missing-file ()
  "An unreadable go.mod yields nil."
  (should-not (golinkname--read-module-path "/no/such/go.mod")))

(ert-deftest golinkname-test-buffer-pkgpath ()
  "Buffer-pkgpath joins the module path with the buffer's relative dir."
  (let* ((tmp (make-temp-file "golinkname-mod-" t))
         (sub (expand-file-name "internal/foo" tmp)))
    (unwind-protect
        (progn
          (make-directory sub t)
          (write-region "module example.com/m\n\ngo 1.21\n" nil
                        (expand-file-name "go.mod" tmp))
          (let ((default-directory (file-name-as-directory sub)))
            (should (equal (golinkname--buffer-pkgpath)
                           "example.com/m/internal/foo"))))
      (delete-directory tmp t))))

(ert-deftest golinkname-test-buffer-pkgpath-at-root ()
  "Buffer-pkgpath at the module root equals the module path."
  (let ((tmp (make-temp-file "golinkname-mod-" t)))
    (unwind-protect
        (progn
          (write-region "module example.com/m\n\ngo 1.21\n" nil
                        (expand-file-name "go.mod" tmp))
          (let ((default-directory (file-name-as-directory tmp)))
            (should (equal (golinkname--buffer-pkgpath) "example.com/m"))))
      (delete-directory tmp t))))

(ert-deftest golinkname-test-buffer-pkgpath-stdlib ()
  "In a stdlib checkout the module prefix is dropped from pkgpaths.
A stdlib checkout has `module std' AND a `builtin/builtin.go' file at
the root; in that case `testing/synctest' is the right import path,
not `std/testing/synctest'.  This must match the Go-side resolver."
  (let* ((tmp (make-temp-file "golinkname-stdlib-" t))
         (sub (expand-file-name "testing/synctest" tmp)))
    (unwind-protect
        (progn
          (make-directory sub t)
          (make-directory (expand-file-name "builtin" tmp) t)
          (write-region "module std\n\ngo 1.26\n" nil
                        (expand-file-name "go.mod" tmp))
          (write-region "package builtin\n" nil
                        (expand-file-name "builtin/builtin.go" tmp))
          (let ((default-directory (file-name-as-directory sub)))
            (should (equal (golinkname--buffer-pkgpath)
                           "testing/synctest"))))
      (delete-directory tmp t))))

(ert-deftest golinkname-test-buffer-pkgpath-stdlib-impostor ()
  "A user module that picks the name `std' is not treated as stdlib.
The check requires both the module name AND a `builtin/builtin.go' at
the root; without the file, the prefix is preserved."
  (let* ((tmp (make-temp-file "golinkname-impostor-" t))
         (sub (expand-file-name "foo" tmp)))
    (unwind-protect
        (progn
          (make-directory sub t)
          (write-region "module std\n\ngo 1.21\n" nil
                        (expand-file-name "go.mod" tmp))
          (let ((default-directory (file-name-as-directory sub)))
            (should (equal (golinkname--buffer-pkgpath) "std/foo"))))
      (delete-directory tmp t))))

;;;; Symbol-at-point

(ert-deftest golinkname-test-symbol-at-point-ascii ()
  "An ASCII identifier under point is returned verbatim."
  (with-temp-buffer
    (insert "func bar() {}\n")
    (goto-char (point-min))
    (search-forward "bar")
    (backward-char 1)
    (should (equal (golinkname--symbol-at-point) "bar"))))

(ert-deftest golinkname-test-symbol-at-point-on-keyword-ok ()
  "Symbol-at-point doesn't filter Go keywords; the lookup will just miss."
  (with-temp-buffer
    (insert "func bar() {}\n")
    (goto-char (point-min))
    (search-forward "func")
    (backward-char 1)
    (should (equal (golinkname--symbol-at-point) "func"))))

;;;; Identifier-at-point (used to pre-fill the prompt)

(ert-deftest golinkname-test-identifier-at-point-on-go-symbol ()
  "On a Go identifier inside a module, identifier-at-point qualifies it."
  (let* ((tmp (make-temp-file "golinkname-mod-" t))
         (file (expand-file-name "b/b.go" tmp)))
    (unwind-protect
        (progn
          (make-directory (file-name-directory file) t)
          (write-region "module example.com/m\n\ngo 1.21\n" nil
                        (expand-file-name "go.mod" tmp))
          (write-region "package b\n\nfunc bar() string { return \"\" }\n"
                        nil file)
          (with-temp-buffer
            (setq buffer-file-name file)
            (setq default-directory (file-name-as-directory
                                     (expand-file-name "b" tmp)))
            (insert-file-contents file)
            (goto-char (point-min))
            (search-forward "bar")
            (backward-char 1)
            (should (equal (golinkname--identifier-at-point)
                           "example.com/m/b.bar"))))
      (delete-directory tmp t))))

(ert-deftest golinkname-test-identifier-at-point-without-go-mod-nil ()
  "Outside a Go module, identifier-at-point returns nil."
  (let ((tmp (make-temp-file "golinkname-nomod-" t)))
    (unwind-protect
        (with-temp-buffer
          (setq default-directory (file-name-as-directory tmp))
          (insert "func bar() {}\n")
          (goto-char (point-min))
          (search-forward "bar")
          (backward-char 1)
          (should-not (golinkname--identifier-at-point)))
      (delete-directory tmp t))))

;;;; JSON parsing

(ert-deftest golinkname-test-parse-records-empty-array ()
  "An empty JSON array parses to nil."
  (should (null (golinkname--parse-records "[]"))))

(ert-deftest golinkname-test-parse-records-nil-input ()
  "Nil input yields nil (no decoder error)."
  (should (null (golinkname--parse-records nil))))

(ert-deftest golinkname-test-parse-records-empty-string ()
  "Empty string yields nil (no decoder error)."
  (should (null (golinkname--parse-records ""))))

(ert-deftest golinkname-test-parse-records-malformed-json ()
  "Malformed JSON yields nil rather than signaling."
  (should (null (golinkname--parse-records "not json"))))

(ert-deftest golinkname-test-parse-records-one-record ()
  "A single record round-trips through the parser."
  (let* ((json (concat "[{\"schemaVersion\":1,\"file\":\"a/a.go\","
                       "\"line\":5,\"col\":1,\"form\":\"two-arg\","
                       "\"localName\":\"foo\",\"declName\":\"foo\","
                       "\"declKind\":\"func\","
                       "\"target\":{\"raw\":\"example.com/m/b.bar\","
                       "\"pkgPath\":\"example.com/m/b\",\"name\":\"bar\","
                       "\"resolved\":[]},"
                       "\"hasUnsafeImport\":true,\"warnings\":[]}]"))
         (records (golinkname--parse-records json)))
    (should (= (length records) 1))
    (let ((r (car records)))
      (should (equal (alist-get 'file r) "a/a.go"))
      (should (= (alist-get 'line r) 5))
      (should (equal (alist-get 'localName r) "foo"))
      (should (equal (alist-get 'raw (alist-get 'target r))
                     "example.com/m/b.bar")))))

;;;; xref-item construction

(ert-deftest golinkname-test-record-to-xref-two-arg ()
  "A two-arg record converts to an xref-item with file location."
  (let* ((record '((schemaVersion . 1)
                   (file . "a/a.go")
                   (line . 7)
                   (col . 1)
                   (form . "two-arg")
                   (localName . "foo")
                   (target . ((raw . "example.com/m/b.bar")
                              (pkgPath . "example.com/m/b")
                              (name . "bar")
                              (resolved . ())))
                   (hasUnsafeImport . t)
                   (warnings . ())))
         (item (golinkname--record-to-xref record "/tmp/mod"))
         (loc (xref-item-location item)))
    (should (string-match-p "foo" (xref-item-summary item)))
    (should (string-match-p "example.com/m/b.bar" (xref-item-summary item)))
    (should (equal (xref-location-group loc) "/tmp/mod/a/a.go"))))

(ert-deftest golinkname-test-record-to-xref-one-arg ()
  "A one-arg record (no target) still produces a valid item."
  (let* ((record '((schemaVersion . 1)
                   (file . "a/a.go")
                   (line . 4)
                   (col . 1)
                   (form . "one-arg")
                   (localName . "foo")
                   (target . nil)
                   (hasUnsafeImport . t)
                   (warnings . ())))
         (item (golinkname--record-to-xref record "/tmp/mod")))
    (should (string-match-p "one-arg" (xref-item-summary item)))))

(ert-deftest golinkname-test-record-to-xref-parse-error-skipped ()
  "Parse-error records have no location and yield nil."
  (let ((record '((schemaVersion . 1)
                  (file . "broken.go")
                  (parseError . "expected ';', found 'EOF'"))))
    (should-not (golinkname--record-to-xref record "/tmp/mod"))))

;;;; Project root resolution

(ert-deftest golinkname-test-project-root-finds-go-mod ()
  "When `default-directory' is nested below a go.mod, root resolves up."
  (let* ((tmp (make-temp-file "golinkname-test-" t))
         (sub (expand-file-name "internal/foo" tmp)))
    (unwind-protect
        (progn
          (make-directory sub t)
          (write-region "module example.com/m\n\ngo 1.21\n" nil
                        (expand-file-name "go.mod" tmp))
          (let ((default-directory (file-name-as-directory sub)))
            ;; locate-dominating-file may resolve through symlinks; both
            ;; the literal path and its truename are acceptable answers.
            (should (member (file-name-as-directory (golinkname--project-root))
                            (list (file-name-as-directory tmp)
                                  (file-name-as-directory (file-truename tmp)))))))
      (delete-directory tmp t))))

;;;; End-to-end against the real binary

(defvar golinkname-tests--bin
  (or (getenv "GOLINKNAME_BIN")
      (let ((repo (locate-dominating-file
                   (or load-file-name buffer-file-name default-directory)
                   "go.mod")))
        (and repo (expand-file-name "bin/golinkname" repo))))
  "Absolute path to the golinkname binary used by end-to-end tests.")

(defun golinkname-tests--ensure-binary ()
  "Build the binary if it is not yet on disk; skip the test if Go is unavailable."
  (unless (and golinkname-tests--bin
               (file-executable-p golinkname-tests--bin))
    (let ((repo (locate-dominating-file
                 (or load-file-name buffer-file-name default-directory)
                 "go.mod")))
      (unless repo
        (ert-skip "no enclosing go.mod, cannot build binary"))
      (unless (executable-find "go")
        (ert-skip "no `go' on PATH, cannot build binary"))
      (let ((default-directory repo)
            (out (expand-file-name "bin/golinkname" repo)))
        (make-directory (file-name-directory out) t)
        (unless (eq 0 (call-process "go" nil nil nil
                                    "build" "-o" out "./cmd/golinkname"))
          (ert-skip "go build failed"))
        (setq golinkname-tests--bin out)))))

(defun golinkname-tests--make-fixture-module ()
  "Materialize a small two-package module on disk and return its root."
  (let* ((tmp (make-temp-file "golinkname-fixture-" t)))
    (write-region "module example.com/m\n\ngo 1.21\n" nil
                  (expand-file-name "go.mod" tmp))
    (make-directory (expand-file-name "a" tmp))
    (make-directory (expand-file-name "b" tmp))
    (write-region (concat "package a\n\n"
                          "import _ \"unsafe\"\n\n"
                          "//go:linkname foo example.com/m/b.bar\n"
                          "func foo() string\n")
                  nil (expand-file-name "a/a.go" tmp))
    (write-region "package b\n\nfunc bar() string { return \"\" }\n"
                  nil (expand-file-name "b/b.go" tmp))
    tmp))

(ert-deftest golinkname-test-end-to-end-refs-from-target-decl ()
  "Refs of `example.com/m/b.bar' include the directive in package a."
  (golinkname-tests--ensure-binary)
  (let* ((module (golinkname-tests--make-fixture-module))
         (golinkname-executable golinkname-tests--bin))
    (unwind-protect
        (with-temp-buffer
          (setq buffer-file-name (expand-file-name "b/b.go" module))
          (setq default-directory (file-name-as-directory
                                   (expand-file-name "b" module)))
          (insert-file-contents buffer-file-name)
          (goto-char (point-min))
          (search-forward "bar")
          (backward-char 1)
          (let* ((identifier (golinkname--identifier-at-point))
                 (refs (and identifier (golinkname--refs identifier))))
            (should (equal identifier "example.com/m/b.bar"))
            (should (= (length refs) 1))
            (let* ((loc (xref-item-location (car refs)))
                   (file (xref-location-group loc)))
              (should (string-match-p "/a/a.go\\'" file)))))
      (delete-directory module t))))

(ert-deftest golinkname-test-end-to-end-refs-from-bridge-local ()
  "Refs from the bridge-local side surface the same directive.
This is the reverse of `golinkname-test-end-to-end-refs-from-target-decl':
standing on `foo' (the directive's local name in package `a') must
return that same directive, because `golinkname-find-references' is
symmetric -- it matches both target and local-side qualified names."
  (golinkname-tests--ensure-binary)
  (let* ((module (golinkname-tests--make-fixture-module))
         (golinkname-executable golinkname-tests--bin))
    (unwind-protect
        (with-temp-buffer
          (setq buffer-file-name (expand-file-name "a/a.go" module))
          (setq default-directory (file-name-as-directory
                                   (expand-file-name "a" module)))
          (insert-file-contents buffer-file-name)
          (goto-char (point-min))
          (search-forward "func foo")
          (backward-char 1)
          (let* ((identifier (golinkname--identifier-at-point))
                 (refs (and identifier (golinkname--refs identifier))))
            (should (equal identifier "example.com/m/a.foo"))
            (should (= (length refs) 1))
            (let* ((loc (xref-item-location (car refs)))
                   (file (xref-location-group loc)))
              (should (string-match-p "/a/a.go\\'" file)))))
      (delete-directory module t))))

(ert-deftest golinkname-test-end-to-end-list ()
  "`golinkname-list' renders one row per directive in the module."
  (golinkname-tests--ensure-binary)
  (let* ((module (golinkname-tests--make-fixture-module))
         (golinkname-executable golinkname-tests--bin)
         (default-directory (file-name-as-directory module)))
    (unwind-protect
        (progn
          (golinkname-list)
          (with-current-buffer "*golinkname*"
            (should (eq major-mode 'golinkname-list-mode))
            (should (= (length tabulated-list-entries) 1))
            (let ((row (cadr (car tabulated-list-entries))))
              (should (string-match-p "a/a.go" (aref row 0)))
              (should (equal (aref row 2) "func/two-arg"))
              (should (equal (aref row 3) "pull"))
              (should (equal (aref row 4) "foo"))
              (should (equal (aref row 5) "example.com/m/b.bar")))))
      (when (get-buffer "*golinkname*") (kill-buffer "*golinkname*"))
      (delete-directory module t))))

(ert-deftest golinkname-test-end-to-end-list-buffer-with-directive ()
  "`golinkname-list-buffer' shows only the visiting file's directives.
The fixture has a directive in a/a.go and none in b/b.go.  Visiting
a/a.go must surface exactly that one record."
  (golinkname-tests--ensure-binary)
  (let* ((module (golinkname-tests--make-fixture-module))
         (golinkname-executable golinkname-tests--bin))
    (unwind-protect
        (with-temp-buffer
          (setq buffer-file-name (expand-file-name "a/a.go" module))
          (setq default-directory (file-name-as-directory
                                   (expand-file-name "a" module)))
          (insert-file-contents buffer-file-name)
          (golinkname-list-buffer)
          (with-current-buffer "*golinkname-buffer*"
            (should (eq major-mode 'golinkname-list-mode))
            (should (= (length tabulated-list-entries) 1))
            (let ((row (cadr (car tabulated-list-entries))))
              (should (string-match-p "a/a.go" (aref row 0))))))
      (when (get-buffer "*golinkname-buffer*")
        (kill-buffer "*golinkname-buffer*"))
      (delete-directory module t))))

(ert-deftest golinkname-test-end-to-end-list-buffer-without-directive ()
  "`golinkname-list-buffer' shows an empty table when the file has no directives.
Visiting b/b.go (which carries no //go:linkname) must yield zero
entries -- a real answer, not an error."
  (golinkname-tests--ensure-binary)
  (let* ((module (golinkname-tests--make-fixture-module))
         (golinkname-executable golinkname-tests--bin))
    (unwind-protect
        (with-temp-buffer
          (setq buffer-file-name (expand-file-name "b/b.go" module))
          (setq default-directory (file-name-as-directory
                                   (expand-file-name "b" module)))
          (insert-file-contents buffer-file-name)
          (golinkname-list-buffer)
          (with-current-buffer "*golinkname-buffer*"
            (should (eq major-mode 'golinkname-list-mode))
            (should (= (length tabulated-list-entries) 0))))
      (when (get-buffer "*golinkname-buffer*")
        (kill-buffer "*golinkname-buffer*"))
      (delete-directory module t))))

(ert-deftest golinkname-test-list-buffer-without-buffer-file ()
  "`golinkname-list-buffer' errors when the buffer is not visiting a file."
  (with-temp-buffer
    (should-error (golinkname-list-buffer) :type 'user-error)))

(ert-deftest golinkname-test-find-references-rejects-bad-input ()
  "`golinkname-find-references' rejects malformed `pkgpath.name' inputs.
Beyond the obvious empty / no-dot cases, the validator also rejects
inputs with empty content on either side of the final dot
(`foo.', `.bar', bare `.')."
  (should-error (golinkname-find-references "nodot") :type 'user-error)
  (should-error (golinkname-find-references "")      :type 'user-error)
  (should-error (golinkname-find-references nil)     :type 'user-error)
  (should-error (golinkname-find-references ".")     :type 'user-error)
  (should-error (golinkname-find-references "foo.")  :type 'user-error)
  (should-error (golinkname-find-references ".bar")  :type 'user-error))

(ert-deftest golinkname-test-valid-target-p ()
  "`golinkname--valid-target-p' is true only for well-formed pkgpath.name.
A pkgpath itself may contain dots (`example.com/foo'), so the rule
checks the *last* dot and that there is non-empty content on both
sides of it."
  (should (golinkname--valid-target-p "pkg.Name"))
  (should (golinkname--valid-target-p "example.com/m/pkg.Name"))
  (should (golinkname--valid-target-p "a.b.c"))
  (should-not (golinkname--valid-target-p ""))
  (should-not (golinkname--valid-target-p nil))
  (should-not (golinkname--valid-target-p "nodot"))
  (should-not (golinkname--valid-target-p "."))
  (should-not (golinkname--valid-target-p "foo."))
  (should-not (golinkname--valid-target-p ".bar")))

(ert-deftest golinkname-test-list-row-two-arg-extern ()
  "A `two-arg-extern' record renders as `var/two-arg-extern' in the Form column.
Regression test: the Form column must be wide enough for the combined
`kind/form' rendering, which is longer than the bare form string."
  (let* ((record '((schemaVersion . 1)
                   (file . "runtime/cgo.go")
                   (line . 12)
                   (col . 1)
                   (form . "two-arg-extern")
                   (direction . "pull")
                   (localName . "_cgo_mmap")
                   (declName . "_cgo_mmap")
                   (declKind . "var")
                   (target . ((raw . "_cgo_mmap")
                              (pkgPath . "")
                              (name . "")
                              (resolved . ())))
                   (hasUnsafeImport . t)
                   (warnings . ())))
         (entry (golinkname--list-entry record))
         (row (cadr entry)))
    (should (equal (aref row 2) "var/two-arg-extern"))
    (should (equal (aref row 3) "pull"))
    ;; The Form column itself must be wide enough to hold the full
    ;; string -- otherwise tabulated-list will silently truncate it.
    (let ((form-col-width
           (cadr (aref (with-temp-buffer
                         (golinkname-list-mode)
                         tabulated-list-format)
                       2))))
      (should (>= form-col-width (length "var/two-arg-extern"))))))

(ert-deftest golinkname-test-list-row-direction-column ()
  "Direction is rendered in the Dir column for each form/body combo.
A push directive (one-arg with body, or two-arg with body) renders as
`push'; a pull directive (bodyless, two-arg-extern, or var) renders as
`pull'; a missing direction (parse-error) renders as `-'."
  (let ((push-record '((schemaVersion . 1)
                       (file . "a.go")
                       (line . 5)
                       (col . 1)
                       (form . "one-arg")
                       (direction . "push")
                       (localName . "foo")
                       (declName . "foo")
                       (declKind . "func")
                       (target . nil)
                       (hasUnsafeImport . t)
                       (warnings . ())))
        (pull-record '((schemaVersion . 1)
                       (file . "b.go")
                       (line . 7)
                       (col . 1)
                       (form . "two-arg")
                       (direction . "pull")
                       (localName . "bar")
                       (declName . "bar")
                       (declKind . "func")
                       (target . ((raw . "pkg.X")
                                  (pkgPath . "pkg")
                                  (name . "X")
                                  (resolved . ())))
                       (hasUnsafeImport . t)
                       (warnings . ())))
        (parse-error-record '((schemaVersion . 1)
                              (file . "broken.go")
                              (parseError . "boom"))))
    (should (equal (aref (cadr (golinkname--list-entry push-record)) 3)
                   "push"))
    (should (equal (aref (cadr (golinkname--list-entry pull-record)) 3)
                   "pull"))
    (should (equal (aref (cadr (golinkname--list-entry parse-error-record)) 3)
                   "-"))))

(provide 'golinkname-tests)

;;; golinkname-tests.el ends here
