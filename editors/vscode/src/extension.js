// Moxie language support.
//
// Diagnostics come from the compiler itself: on save (and on demand) the
// package's directory is built with `moxie build`, and its positioned errors
// become problems. Completion, hover and go-to-definition are served from the
// same keyword/builtin sets the compiler's own token package defines plus a
// workspace index of declarations, so there is no second grammar to drift.

const vscode = require("vscode");
const cp = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");

const KEYWORDS = {
	break: "Leave the innermost for, switch or select.",
	case: "A switch or select arm.",
	chan: "A channel. Channels are fixed at spawn time; there is no make().",
	const: "A compile-time constant.",
	continue: "Continue the innermost for loop.",
	default: "The default arm of a switch or select.",
	defer: "Run at function return.",
	else: "The else branch of an if.",
	for: "The only loop. There is no while and no do.",
	goto: "A jump to a label.",
	if: "A conditional. The init statement is allowed: if x, ok := v.(T); ok { … }.",
	import: "Import a package. Blank imports are legal, and are how //:linkname is enabled.",
	interface: "A method set. There is no empty interface and no any.",
	map: "A hashmap type.",
	package: "The package clause, first non-comment line of a file.",
	range: "Ranges over a slice, map, string (bytes, not runes) or channel.",
	return: "Return from a function. Every return value is named and return is bare.",
	select: "Wait on several channel operations.",
	struct: "A struct type.",
	switch: "A switch on a tag or on true.",
	type: "A type declaration.",
	var: "A package- or function-scope variable.",
	true: "The boolean constant true.",
	false: "The boolean constant false.",
	nil: "The zero value for a pointer, slice, map, chan or func.",
	iota: "The constant counter inside a const block.",
};

const BUILTINS = {
	push: "Append one element. Bounded: it fails loud past the end of capacity.",
	pop: "Remove and return the last element.",
	resize: "Change a slice's length within its capacity.",
	len: "The length of a slice, map, string or channel.",
	cap: "The capacity of a slice or channel.",
	copy: "Copy between slices.",
	delete: "Delete a map key.",
	close: "Close a channel; every holder sees the close signal.",
	clear: "Zero a map or slice.",
	min: "The smaller of two ordered values.",
	max: "The larger of two ordered values.",
	panic: "Abort the domain with a string.",
	recover: "Resume from a panic inside a deferred call.",
	print: "Write to stderr without formatting.",
	println: "Write a line to stderr.",
	spawn: "Start a new domain. Channels are bound at spawn time; a closure cannot be spawned.",
};

const TYPES = {
	bool: "Boolean.",
	byte: "uint8, and byte is an alias for it.",
	rune: "int32, and rune is an alias for it.",
	string: "A mutable byte slice: string and []byte are one type.",
	error: "The error interface: Error() string (and String() so fmt can print it).",
	uintptr: "The pointer-sized integer for unsafe.Pointer arithmetic.",
	comparable: "The type-set constraint for == and !=.",
	int8: "8-bit signed integer.",
	int16: "16-bit signed integer.",
	int32: "32-bit signed integer; len() and indexes are this.",
	int64: "64-bit signed integer.",
	uint8: "8-bit unsigned integer.",
	uint16: "16-bit unsigned integer.",
	uint32: "32-bit unsigned integer.",
	uint64: "64-bit unsigned integer.",
	float32: "32-bit float.",
	float64: "64-bit float.",
};

const REMOVED = {
	int: "Not predeclared in Moxie: use an explicit width. You may declare your own int.",
	uint: "Not predeclared in Moxie: use an explicit width.",
	complex64: "Removed from Moxie.",
	complex128: "Removed from Moxie.",
	make: "Removed: presize with []T{:n:cap}.",
	new: "Removed: use &T{} with an arena.",
	append: "Removed: use push, with mxutil.Ensure for a spread.",
	complex: "Removed from Moxie.",
	real: "Removed from Moxie.",
	imag: "Removed from Moxie.",
	reflect: "Removed from Moxie.",
	any: "The empty interface is not available; constrain a type parameter concretely.",
};

let channel;
let diagnostics;
let buildTimer;
let building = false;

function config() {
	return vscode.workspace.getConfiguration("moxie");
}

function compilerRoot(scopeUri) {
	const configured = (config().get("moxieRoot") || "").trim();
	if (configured) {
		return configured;
	}
	const folder = scopeUri ? vscode.workspace.getWorkspaceFolder(scopeUri) : undefined;
	return folder ? folder.uri.fsPath : "";
}

// packageDir returns the directory to build for a document: its own directory
// when that holds .mx files, otherwise the workspace folder.
function packageDir(doc) {
	const dir = path.dirname(doc.uri.fsPath);
	try {
		if (fs.readdirSync(dir).some((f) => f.endsWith(".mx"))) {
			return dir;
		}
	} catch (_) {
		/* fall through */
	}
	const folder = vscode.workspace.getWorkspaceFolder(doc.uri);
	return folder ? folder.uri.fsPath : dir;
}

function runCompiler(dir, scopeUri) {
	return new Promise((resolve) => {
		const exe = config().get("moxie.path") || "moxie";
		const out = path.join(os.tmpdir(), "moxie-lang-diagnostics");
		const extra = config().get("moxie.buildFlags") || [];
		const args = ["build"].concat(extra).concat(["-o", out, "."]);
		const env = Object.assign({}, process.env);
		const root = compilerRoot(scopeUri);
		if (root) {
			env.MOXIEROOT = root;
		}
		const child = cp.spawn(exe, args, { cwd: dir, env });
		let text = "";
		child.stdout.on("data", (d) => (text += d.toString()));
		child.stderr.on("data", (d) => (text += d.toString()));
		child.on("error", (err) => resolve({ text: err.message, dir }));
		child.on("close", () => resolve({ text, dir }));
	});
}

function parseErrors(text) {
	const found = [];
	for (const raw of text.split(/\r?\n/)) {
		const line = raw.trim();
		if (!line || line.startsWith(";") || line.startsWith(">>>") || line.startsWith("[mem]")) {
			continue;
		}
		let m = /^(.*?):(\d+):(\d+):\s*(.*)$/.exec(line);
		if (m) {
			found.push({ file: m[1], line: Number(m[2]), col: Number(m[3]), msg: m[4] });
			continue;
		}
		m = /^(.*?):(\d+):\s*(.*)$/.exec(line);
		if (m) {
			found.push({ file: m[1], line: Number(m[2]), col: 1, msg: m[3] });
		}
	}
	return found;
}

function resolveFile(file, dir) {
	if (path.isAbsolute(file)) {
		return vscode.Uri.file(file);
	}
	const candidate = path.join(dir, file);
	if (fs.existsSync(candidate)) {
		return vscode.Uri.file(candidate);
	}
	const folder = vscode.workspace.workspaceFolders;
	if (folder && folder.length) {
		const other = path.join(folder[0].uri.fsPath, file);
		if (fs.existsSync(other)) {
			return vscode.Uri.file(other);
		}
	}
	return vscode.Uri.file(candidate);
}

async function diagnose(doc, quiet) {
	if (building) {
		return;
	}
	building = true;
	try {
		const dir = packageDir(doc);
		const res = await runCompiler(dir, doc.uri);
		if (config().get("moxie.trace") !== "off") {
			channel.appendLine("$ moxie build -o <tmp> .  (cwd " + dir + ")");
			channel.appendLine(res.text.trim());
		}
		diagnostics.clear();
		const byFile = new Map();
		for (const e of parseErrors(res.text)) {
			const uri = resolveFile(e.file, res.dir);
			const key = uri.toString();
			if (!byFile.has(key)) {
				byFile.set(key, { uri, items: [] });
			}
			const range = new vscode.Range(
				Math.max(0, e.line - 1),
				Math.max(0, e.col - 1),
				Math.max(0, e.line - 1),
				Math.max(0, e.col)
			);
			byFile.get(key).items.push(
				new vscode.Diagnostic(range, e.msg, vscode.DiagnosticSeverity.Error)
			);
		}
		for (const { uri, items } of byFile.values()) {
			diagnostics.set(uri, items);
		}
		const total = Array.from(byFile.values()).reduce((n, f) => n + f.items.length, 0);
		if (total === 0 && !quiet) {
			channel.appendLine("moxie build: no errors");
		}
	} finally {
		building = false;
	}
}

function declarationIndex() {
	const index = new Map();
	return vscode.workspace.findFiles("**/*.mx", "**/{node_modules,.git,tmp}/**").then((files) => {
		const reads = files.map((uri) =>
			vscode.workspace.openTextDocument(uri).then((doc) => {
				const text = doc.getText();
				const re = /^\s*(?:func\s+(?:\([^)]*\)\s+)?|type\s+|var\s+|const\s+)([A-Za-z_][A-Za-z0-9_]*)/gm;
				let m;
				while ((m = re.exec(text)) !== null) {
					const name = m[1];
					if (!index.has(name)) {
						index.set(name, []);
					}
					const pos = doc.positionAt(m.index + m[0].length - name.length);
					index.get(name).push({ uri, pos, line: doc.lineAt(pos.line).text.trim() });
				}
				return null;
			})
		);
		return Promise.all(reads).then(() => index);
	});
}

function activate(context) {
	channel = vscode.window.createOutputChannel("Moxie");
	diagnostics = vscode.languages.createDiagnosticCollection("moxie");
	context.subscriptions.push(channel, diagnostics);

	const selector = { language: "moxie", scheme: "file" };

	context.subscriptions.push(
		vscode.workspace.onDidSaveTextDocument((doc) => {
			if (doc.languageId !== "moxie" || !config().get("moxie.buildOnSave")) {
				return;
			}
			if (buildTimer) {
				clearTimeout(buildTimer);
			}
			buildTimer = setTimeout(() => diagnose(doc, true), 250);
		})
	);
	context.subscriptions.push(vscode.workspace.onDidOpenTextDocument((doc) => {
		if (doc.languageId === "moxie") {
			diagnose(doc, true);
		}
	}));

	context.subscriptions.push(
		vscode.commands.registerCommand("moxie.build", () => {
			const doc = vscode.window.activeTextEditor && vscode.window.activeTextEditor.document;
			if (!doc || doc.languageId !== "moxie") {
				vscode.window.showInformationMessage("Open a .mx file to build its package.");
				return;
			}
			channel.show(true);
			diagnose(doc, false);
		})
	);

	context.subscriptions.push(
		vscode.languages.registerCompletionItemProvider(selector, {
			provideCompletionItems() {
				const items = [];
				const add = (label, kind, doc) => {
					const item = new vscode.CompletionItem(label, kind);
					item.documentation = new vscode.MarkdownString(doc);
					items.push(item);
				};
				for (const k of Object.keys(KEYWORDS)) {
					add(k, vscode.CompletionItemKind.Keyword, KEYWORDS[k]);
				}
				for (const b of Object.keys(BUILTINS)) {
					add(b, vscode.CompletionItemKind.Function, BUILTINS[b]);
				}
				for (const t of Object.keys(TYPES)) {
					add(t, vscode.CompletionItemKind.TypeParameter, TYPES[t]);
				}
				return items;
			},
		})
	);

	context.subscriptions.push(
		vscode.languages.registerHoverProvider(selector, {
			provideHover(doc, position) {
				const range = doc.getWordRangeAtPosition(position, /[A-Za-z_][A-Za-z0-9_]*/);
				if (!range) {
					return null;
				}
				const word = doc.getText(range);
				const help = KEYWORDS[word] || BUILTINS[word] || TYPES[word] || REMOVED[word];
				if (help) {
					return new vscode.Hover(new vscode.MarkdownString("**" + word + "** — " + help));
				}
				return null;
			},
		})
	);

	const indexPromise = declarationIndex();
	context.subscriptions.push(
		vscode.languages.registerDefinitionProvider(selector, {
			async provideDefinition(doc, position) {
				const range = doc.getWordRangeAtPosition(position, /[A-Za-z_][A-Za-z0-9_]*/);
				if (!range) {
					return null;
				}
				const word = doc.getText(range);
				const index = await indexPromise;
				const hits = index.get(word) || [];
				return hits.map((h) => new vscode.Location(h.uri, h.pos));
			},
		})
	);
}

function deactivate() {
	if (diagnostics) {
		diagnostics.dispose();
	}
}

module.exports = { activate, deactivate };
