;;; golinkname.el --- Resolves //go:linkname directives -*- lexical-binding: t; -*-

;; Copyright (c) 2026 Marin Atanasov Nikolov <dnaeon@gmail.com>
;; SPDX-License-Identifier: BSD-3-Clause

;; Author: Marin Atanasov Nikolov <dnaeon@gmail.com>
;; URL: https://github.com/dnaeon/golinkname
;; Version: 0.1.0
;; Package-Requires: ((emacs "29.1"))
;; Keywords: tools, languages, go

;;; Commentary:

;; This package surfaces references for Go's `//go:linkname' compiler
;; directives in both directions -- the gap `gopls' does not cover.
;; Given a Go symbol (a target, a pull-side bridge, or a push-side
;; bridge), it returns every directive in the current module related to
;; it.  Gopls handles forward navigation from a directive's target
;; argument, but never the inverse, and never the symmetric "find both
;; ends of this bridge" query.
;;
;; The package shells out to the `golinkname' command-line tool, which
;; must be installed separately and on `exec-path'.  Results are
;; presented through the standard `xref' UI, but the package does *not*
;; register itself as an Xref backend. Instead, the package exposes four
;; `M-x'-driven commands; the `*xref*' buffer used by
;; `golinkname-find-references' contains linkname results only.
;;
;; Setup: install the binary, then load the package via your usual
;; mechanism (see editor/emacs/README.md).
;;
;; Interactive entry points (M-x):
;;
;;   `golinkname-find-references'  show every directive in the module
;;       related to the symbol at point: directives whose target is the
;;       symbol, and directives whose own local-side qualified name is
;;       the symbol.  If point is not on a qualifiable Go identifier,
;;       prompts for `pkgpath.name'.
;;   `golinkname-list'  workspace-wide tabulated view of every
;;       directive in the current module.
;;   `golinkname-list-buffer'  same as `golinkname-list' but
;;       restricted to the file the current buffer is visiting.
;;   `golinkname-diagnose'  short status report for debugging.

;;; Code:

(require 'cl-lib)
(require 'project)
(require 'seq)
(require 'tabulated-list)
(require 'xref)

;;;; Customization

;;;###autoload
(defgroup golinkname nil
  "Reverse references for Go //go:linkname directives."
  :group 'tools
  :prefix "golinkname-")

;;;###autoload
(defcustom golinkname-executable "golinkname"
  "Name of, or path to, the `golinkname' command-line binary."
  :type 'string
  :group 'golinkname)

;;;###autoload
(defcustom golinkname-extra-args nil
  "Extra arguments passed to every `golinkname' invocation.
Useful when the caller wants to constrain the tool, for example to
pass `--dir' explicitly when `default-directory' is not inside the
target module."
  :type '(repeat string)
  :group 'golinkname)

;;;; Internal helpers

(defun golinkname--project-root ()
  "Return the root directory used to invoke `golinkname'.
Prefer `project.el'; fall back to walking up for a `go.mod'; finally
fall back to `default-directory'."
  (or (when-let ((proj (project-current nil)))
        (expand-file-name (project-root proj)))
      (when-let ((dir (locate-dominating-file default-directory "go.mod")))
        (expand-file-name dir))
      default-directory))

(defun golinkname--read-module-path (gomod)
  "Read the `module' line from GOMOD and return the module import path.
Returns nil if the file is unreadable or has no recognizable `module'
declaration.  Both bare and double-quoted paths are accepted; everything
after the path on the same line is ignored.  The block-form
`module ( ... )' is not recognised -- it is collapsed by `gofmt' and
does not appear in real-world `go.mod' files."
  (when (file-readable-p gomod)
    (with-temp-buffer
      (insert-file-contents gomod)
      (goto-char (point-min))
      (when (re-search-forward
             "^[[:space:]]*module[[:space:]]+\\(?:\"\\([^\"]+\\)\"\\|\\([^[:space:]]+\\)\\)"
             nil t)
        (or (match-string 1) (match-string 2))))))

(defun golinkname--stdlib-root-p (root)
  "Return non-nil when ROOT is a Go stdlib checkout.
Mirrors the Go-side detection in `pkg/linkname/module.go': the module
must be named exactly `std' AND a `builtin/builtin.go' file must exist
at the root.  The second check rules out a hypothetical user module
that picks the name `std' by accident."
  (let ((gomod (expand-file-name "go.mod" root)))
    (and (file-readable-p gomod)
         (equal (golinkname--read-module-path gomod) "std")
         (file-readable-p (expand-file-name "builtin/builtin.go" root)))))

(defun golinkname--buffer-pkgpath ()
  "Return the import path of the package containing the current buffer.
Returns nil when no enclosing `go.mod' is found, when the module path
cannot be parsed, or when the buffer lives outside the module tree.

Stdlib special-case: when the enclosing module is the Go standard
library (`module std' + `builtin/builtin.go' at the root), import paths
are unprefixed (e.g. `testing/synctest', not `std/testing/synctest').
This must match the Go-side resolver, which treats stdlib paths the
same way; otherwise `golinkname refs' is invoked with a path that
never matches a record."
  (when-let* ((root (golinkname--project-root))
              (gomod (expand-file-name "go.mod" root))
              ((file-readable-p gomod))
              (module (golinkname--read-module-path gomod))
              (rel (file-relative-name
                    (file-name-as-directory default-directory)
                    (file-name-as-directory root))))
    (let ((stdlib (golinkname--stdlib-root-p root)))
      (cond
       ((string-prefix-p ".." rel) nil)
       ((or (string= rel "./") (string= rel ""))
        (if stdlib "builtin" module))
       (stdlib (directory-file-name rel))
       (t (concat module "/" (directory-file-name rel)))))))

(defconst golinkname--go-symbol-re
  "[[:alpha:]_][[:alnum:]_]*"
  "Regexp matching a Go identifier (ASCII subset; sufficient for our use).
Go technically allows non-ASCII letters, but linkname targets in
practice are ASCII.  False negatives here only mean the user is asked
to type the identifier manually.")

(defun golinkname--symbol-at-point ()
  "Return the Go identifier at point, or nil.
Restricted to ASCII identifiers (see `golinkname--go-symbol-re')."
  (save-excursion
    (let ((sym (thing-at-point 'symbol t)))
      (and sym
           (string-match-p (concat "^" golinkname--go-symbol-re "$") sym)
           sym))))

(defun golinkname--identifier-at-point ()
  "Return `pkgpath.name' for the Go symbol at point, or nil.
Returns nil when point is not on a Go identifier, or when no enclosing
module can be resolved.  Used to pre-fill prompts; not a hard gate."
  (when-let ((name (golinkname--symbol-at-point))
             (pkg (golinkname--buffer-pkgpath)))
    (concat pkg "." name)))

(defun golinkname--call (&rest args)
  "Invoke `golinkname' with ARGS, returning stdout as a string.
Returns nil on any process error (binary missing, non-zero exit,
no enclosing module).  Errors are intentionally swallowed so the
calling command shows \"No references found\" rather than a backtrace;
diagnostics go through `golinkname-diagnose' instead."
  (let ((default-directory (golinkname--project-root)))
    (condition-case nil
        (with-output-to-string
          (with-current-buffer standard-output
            (let ((exit (apply #'process-file
                               golinkname-executable nil t nil
                               (append golinkname-extra-args args))))
              (unless (eq exit 0)
                (signal 'error (list "golinkname exited" exit))))))
      (error nil))))

(defun golinkname--parse-records (json-string)
  "Parse JSON-STRING (a `golinkname' array) into a list of alists.
Returns nil when JSON-STRING is nil, empty, or unparseable.  Uses the
native `json-parse-string' (libjansson-backed) for speed and stricter
conformance than the legacy `json.el' implementation."
  (when (and json-string (not (string-empty-p json-string)))
    (condition-case nil
        (append
         (json-parse-string json-string
                            :array-type 'list
                            :object-type 'alist
                            :null-object nil
                            :false-object nil)
         nil)
      (error nil))))

(defun golinkname--record-to-xref (record root)
  "Convert RECORD (alist from JSON) into an `xref-item'.
ROOT is the absolute path of the module root, used to resolve the
record's relative file path.  Returns nil for parse-error records,
which carry no location."
  (let ((parse-error (alist-get 'parseError record))
        (file (alist-get 'file record))
        (line (alist-get 'line record))
        (col (alist-get 'col record)))
    (unless (or parse-error (not file) (not line))
      (let* ((abs (expand-file-name file root))
             (target (alist-get 'target record))
             (target-raw (and target (alist-get 'raw target)))
             (local (alist-get 'localName record))
             (summary (if target-raw
                          (format "%s -> %s" local target-raw)
                        (format "%s (one-arg)" local))))
        (xref-make summary
                   (xref-make-file-location abs line (max 0 (1- (or col 1)))))))))

(defun golinkname--refs (target)
  "Return a list of `xref-item' objects for TARGET in the current module.
TARGET is a `pkgpath.name' string.

Calls `golinkname related', which is symmetric: it returns directives
whose target matches TARGET *and* directives whose own local-side
qualified name matches.  This makes the command work whether point is
on the canonical decl, on the pull-side bridge, or on the push-side
bridge -- one keystroke surfaces everything related."
  (let* ((root (golinkname--project-root))
         (records (golinkname--parse-records
                   (golinkname--call "related" target))))
    (seq-keep (lambda (r) (golinkname--record-to-xref r root)) records)))

;;;; Interactive commands

(defun golinkname--valid-target-p (target)
  "Return non-nil when TARGET looks like a `pkgpath.name' string.
Requires non-empty content on both sides of the final `.': pkgpath
itself may contain dots (`example.com/foo'), so the check is on the
*last* dot, and only that the trailing name is non-empty.  Catches
typos like `foo.', `.bar', and bare `.'."
  (and (stringp target)
       (let ((dot (and (not (string-empty-p target))
                       (cl-position ?. target :from-end t))))
         (and dot (> dot 0) (< dot (1- (length target)))))))

;;;###autoload
(defun golinkname-find-references (&optional target)
  "Show every directive in the current module related to TARGET.
TARGET is a `pkgpath.name' string (e.g. `runtime.gopark').  When
called interactively, the qualified name of the Go identifier at
point is used; if there isn't one, the user is prompted for a
`pkgpath.name' (with empty default).

The match is symmetric: the result includes directives whose
*target* is TARGET (the original \"who pulls/pushes to me?\" query)
*and* directives whose own *local-side* qualified name is TARGET.
That makes the command work in both directions -- standing on the
canonical decl, on a pull-side bridge, or on a push-side bridge all
surface the full set of related sites in one keystroke.  Results
are presented in the standard `*xref*' buffer."
  (interactive
   (list (or (golinkname--identifier-at-point)
             (read-string "Linkname target (pkgpath.name): "))))
  (unless (golinkname--valid-target-p target)
    (user-error "%S is not a valid pkgpath.name" target))
  (let ((fetcher (lambda () (golinkname--refs target))))
    (xref-show-xrefs fetcher nil)))

;;;###autoload
(defun golinkname-list ()
  "Show a tabulated view of every `//go:linkname' directive in the module.
Each row is one directive: file, line, form, local name, target, and
warnings.  Press RET on a row to visit its source location."
  (interactive)
  (let* ((root (golinkname--project-root))
         (records (or (golinkname--parse-records
                       (golinkname--call "index"))
                      (user-error "No directives found, or `golinkname' invocation failed (try `M-x golinkname-diagnose')"))))
    (golinkname--list-show records root "*golinkname*")))

;;;###autoload
(defun golinkname-list-buffer ()
  "Show a tabulated view of `//go:linkname' directives in the current buffer.
Module-wide companion to `golinkname-list', but restricted to the
file the current buffer is visiting.  Useful as a quick \"what
linkname directives live in this file\" query without scrolling
through the module-wide list.

The result lands in `*golinkname-buffer*' so it coexists with the
module-wide `*golinkname*' buffer.  Errors when the buffer is not
visiting a file inside a Go module."
  (interactive)
  (let* ((file buffer-file-name)
         (root (golinkname--project-root)))
    (unless file
      (user-error "Buffer is not visiting a file"))
    (let* ((rel (file-relative-name
                 file (file-name-as-directory root))))
      (when (string-prefix-p ".." rel)
        (user-error "Buffer file is not inside the module rooted at %s" root))
      (let* (;; CLI expects slash-separated paths; on Windows
             ;; `file-relative-name' may return backslashes.
             (rel-slash (replace-regexp-in-string "\\\\" "/" rel))
             (records (golinkname--parse-records
                       (golinkname--call "index" "--file" rel-slash))))
        (golinkname--list-show (or records '()) root "*golinkname-buffer*")))))

(defun golinkname--list-show (records root buffer-name)
  "Render RECORDS in a `golinkname-list-mode' buffer rooted at ROOT.
BUFFER-NAME is the name of the destination buffer (e.g. `*golinkname*'
for the module-wide view, `*golinkname-buffer*' for the buffer-scoped
view)."
  (let ((buf (get-buffer-create buffer-name)))
    (with-current-buffer buf
      (golinkname-list-mode)
      (setq-local golinkname--list-root root)
      (setq tabulated-list-entries
            (mapcar #'golinkname--list-entry records))
      (tabulated-list-print t))
    (pop-to-buffer buf)))

(defun golinkname--list-entry (record)
  "Build a tabulated-list entry from RECORD.
The row id is `(file line)' so `golinkname-list-visit' can look up the
location; the displayed columns are file, line, kind/form, direction,
local name, target, and warnings.

The Form column renders as `kind/form' (e.g. `func/two-arg',
`var/two-arg-extern') so the declaration kind is visible without
adding a separate column to an already wide table."
  (let* ((file (alist-get 'file record))
         (line (alist-get 'line record))
         (parse-error (alist-get 'parseError record))
         (form (or (alist-get 'form record) ""))
         (kind (or (alist-get 'declKind record) ""))
         (kind-form (concat (if (string-empty-p kind) "-" kind)
                            "/"
                            (if (string-empty-p form) "-" form)))
         (direction (or (alist-get 'direction record) ""))
         (dir-str (if (string-empty-p direction) "-" direction))
         (local (or (alist-get 'localName record) ""))
         (target-raw (or (alist-get 'raw (alist-get 'target record)) ""))
         (warnings (or (alist-get 'warnings record) ()))
         (warn-str (if warnings (string-join warnings ",") ""))
         (id (list file (or line 0))))
    (if parse-error
        (list id (vector (or file "") "" "parse-error" "-" "" "" parse-error))
      (list id (vector (or file "")
                       (if line (number-to-string line) "")
                       kind-form dir-str local target-raw warn-str)))))

(defvar golinkname--list-root nil
  "Module root recorded by `golinkname-list' so RET can resolve files.")

(defun golinkname-list-visit ()
  "Visit the directive at point in the *golinkname* buffer."
  (interactive)
  (let* ((id (tabulated-list-get-id))
         (file (and id (car id)))
         (line (and id (cadr id))))
    (unless (and file (not (string-empty-p file)))
      (user-error "No location at point"))
    (let ((abs (expand-file-name file (or golinkname--list-root
                                          default-directory))))
      (find-file-other-window abs)
      (when (and line (> line 0))
        (goto-char (point-min))
        (forward-line (1- line))))))

(defvar golinkname-list-mode-map
  (let ((map (make-sparse-keymap)))
    (set-keymap-parent map tabulated-list-mode-map)
    (define-key map (kbd "RET") #'golinkname-list-visit)
    (define-key map (kbd "o")   #'golinkname-list-visit)
    map)
  "Keymap for `golinkname-list-mode'.")

(define-derived-mode golinkname-list-mode tabulated-list-mode "golinkname-list"
  "Major mode for `golinkname-list' results."
  (setq tabulated-list-format
        [("File"     32 t)
         ("Line"      6 t :right-align t)
         ("Form"     20 t)
         ("Dir"       6 t)
         ("Local"    16 t)
         ("Target"   40 t)
         ("Warnings"  0 t)])
  (setq tabulated-list-padding 1)
  (setq tabulated-list-sort-key '("File" . nil))
  (tabulated-list-init-header))

;;;; User-facing diagnostics

;;;###autoload
(defun golinkname-diagnose ()
  "Print a short status report about the `golinkname' setup.
Shows the resolved binary, project root, and whether the binary can
be invoked successfully.  Useful for debugging when a command returns
no results unexpectedly."
  (interactive)
  (let* ((bin (executable-find golinkname-executable))
         (root (golinkname--project-root))
         (default-directory root)
         (output (and bin (golinkname--call "index"))))
    (message
     (concat "golinkname-diagnose:\n"
             (format "  executable    : %s%s\n"
                     golinkname-executable
                     (if bin (format " (resolved: %s)" bin) " (NOT FOUND)"))
             (format "  project root  : %s\n" root)
             (format "  invocation    : %s"
                     (cond ((not bin) "skipped (binary missing)")
                           ((null output) "FAILED (non-zero exit or no module)")
                           (t (format "ok (%d bytes of JSON)"
                                      (length output)))))))))

(provide 'golinkname)

;;; golinkname.el ends here
