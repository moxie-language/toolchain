MOXIEROOT ?= ..
MOXIE ?= $(MOXIEROOT)/moxie

.PHONY: all clean test test-hello test-parsemxh test-goenv test-compileopts test-syntax test-typecheck test-mxssa test-loader test-llvm test-glue test-compile

all: hello.so parsemxh.so goenv_version.so compileopts.so syntax.so typecheck.so mxssa.so loader.so compile.so llvm-glue/libmoxie-llvm-glue.so

hello.so: hello/hello.mx hello/moxie.mod
	cd $(MOXIEROOT) && MOXIEROOT=. ./moxie build -buildmode=c-shared -o bootstrap/hello.so ./bootstrap/hello/

parsemxh.so: parsemxh/parsemxh.mx parsemxh/moxie.mod
	cd $(MOXIEROOT) && MOXIEROOT=. ./moxie build -buildmode=c-shared -o bootstrap/parsemxh.so ./bootstrap/parsemxh/

llvm-glue/libmoxie-llvm-glue.so:
	./llvm-glue/build.sh

goenv_version.so: goenv_version/version.mx goenv_version/moxie.mod
	cd $(MOXIEROOT) && MOXIEROOT=. ./moxie build -buildmode=c-shared -o bootstrap/goenv_version.so ./bootstrap/goenv_version/

compileopts.so: compileopts/compileopts.mx compileopts/moxie.mod
	cd $(MOXIEROOT) && MOXIEROOT=. ./moxie build -buildmode=c-shared -o bootstrap/compileopts.so ./bootstrap/compileopts/

syntax.so: syntax/tokens.mx syntax/pos.mx syntax/source.mx syntax/nodes.mx syntax/scanner.mx syntax/parser.mx syntax/syntax.mx syntax/exports.mx syntax/moxie.mod
	cd $(MOXIEROOT) && MOXIEROOT=. ./moxie build -buildmode=c-shared -o bootstrap/syntax.so ./bootstrap/syntax/

typecheck.so: typecheck/*.mx typecheck/moxie.mod
	cd $(MOXIEROOT) && MOXIEROOT=. ./moxie build -gc=leaking -buildmode=c-shared -o bootstrap/typecheck.so ./bootstrap/typecheck/

mxssa.so: mxssa/*.mx mxssa/moxie.mod
	cd $(MOXIEROOT) && MOXIEROOT=. ./moxie build -gc=leaking -buildmode=c-shared -o bootstrap/mxssa.so ./bootstrap/mxssa/

loader.so: loader/*.mx loader/moxie.mod
	cd $(MOXIEROOT) && MOXIEROOT=. ./moxie build -gc=leaking -buildmode=c-shared -o bootstrap/loader.so ./bootstrap/loader/

compile.so: compile/*.mx compile/moxie.mod
	cd $(MOXIEROOT) && MOXIEROOT=. ./moxie build -gc=leaking -buildmode=c-shared -o bootstrap/compile.so ./bootstrap/compile/

test: test-hello test-parsemxh test-goenv test-compileopts test-syntax test-typecheck test-mxssa test-loader test-llvm test-compile

test-hello: hello.so
	cd probe && go run . $(CURDIR)/hello.so

test-parsemxh: parsemxh.so
	cd probe_parsemxh && go run . $(CURDIR)/parsemxh.so

test-goenv: goenv_version.so
	cd probe_goenv && go run . $(CURDIR)/goenv_version.so

test-compileopts: compileopts.so
	cd probe_compileopts && go run . $(CURDIR)/compileopts.so

test-syntax: syntax.so
	cd probe_syntax && go run . $(CURDIR)/syntax.so

test-typecheck: typecheck.so
	cd probe_typecheck && go run . $(CURDIR)/typecheck.so

test-mxssa: mxssa.so
	cd probe_mxssa && go run . $(CURDIR)/mxssa.so

test-loader: loader.so
	cd probe_loader && go run . $(CURDIR)/loader.so

test-llvm: llvm-glue/libmoxie-llvm-glue.so
	cd probe_llvm && go run . $(CURDIR)/llvm-glue/libmoxie-llvm-glue.so

test-compile: compile.so
	cd probe_compile && go run . $(CURDIR)/compile.so

clean:
	rm -f hello.so parsemxh.so goenv_version.so compileopts.so syntax.so typecheck.so mxssa.so loader.so compile.so llvm-glue/libmoxie-llvm-glue.so
