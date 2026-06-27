# Moxie IntelliJ Plugin

IntelliJ IDEA plugin for Moxie language support (.mx files).

## Features

- **Syntax Highlighting**: Full syntax highlighting for Moxie language
  - Keywords (func, var, const, etc.)
  - Built-in functions (push, pop, resize, cap, clear, close, copy, delete, len, max, min, panic, print, println, recover, spawn)
  - Built-in types (int32, int64, uint32, uint64, string, bool, float32, float64, etc.)
  - Comments (line and block)
  - String literals (including raw strings)
  - Numbers (integers, floats)
  - Operators and delimiters

- **File Type Recognition**: Automatically recognizes .mx files as Moxie

- **Editor Features**:
  - Brace matching ((), {}, [])
  - Code folding (blocks and comments)
  - Comment/uncomment support (Ctrl+/ for line, Ctrl+Shift+/ for block)
  - Automatic quote pairing

- **Customizable Colors**: Configure syntax colors in Settings → Editor → Color Scheme → Moxie

## Installation

### From Source

1. Clone the repository
2. Open in IntelliJ IDEA
3. Run `./gradlew buildPlugin`
4. Install the plugin from `build/distributions/moxie-intellij-plugin-*.zip` via Settings → Plugins → Install Plugin from Disk

### Building

```bash
./gradlew buildPlugin
```

The plugin will be generated in `build/distributions/`.

## Development

### Requirements

- IntelliJ IDEA 2023.2 or later
- JDK 17 or later
- Gradle 8.5 or later

### Project Structure

```
src/main/
├── kotlin/com/moxie/lang/
│   ├── MoxieLanguage.kt              # Language definition
│   ├── MoxieFileType.kt              # File type definition
│   ├── MoxieLexer.kt                 # Lexical analyzer
│   ├── MoxieTokenTypes.kt            # Token definitions
│   ├── MoxieParser.kt                # Parser
│   ├── MoxieParserDefinition.kt      # Parser configuration
│   ├── MoxieSyntaxHighlighter.kt     # Syntax highlighter
│   ├── MoxieColorSettingsPage.kt     # Color settings UI
│   ├── MoxieCommenter.kt             # Comment handler
│   ├── MoxieBraceMatcher.kt          # Brace matching
│   ├── MoxieFoldingBuilder.kt        # Code folding
│   └── MoxieQuoteHandler.kt          # Quote handling
└── resources/
    ├── META-INF/plugin.xml            # Plugin configuration
    └── icons/moxie-file.svg           # File icon
```

### Running in Development

```bash
./gradlew runIde
```

This will launch a new IntelliJ IDEA instance with the plugin loaded.

## Moxie Language Features Supported

- Go keywords and syntax (minus `go` and `fallthrough`, which are compile errors in Moxie)
- Moxie-specific builtin: `spawn()` — creates isolated domains over IPC
- `string` and `[]byte` are the same type
- Slice, channel, and map literal syntax: `[]T{:n}`, `chan T{}`, `map[K]V{}`
- Removed from Go (not highlighted as keywords/builtins, so misuse stands out):
  - `go`, `fallthrough`
  - `append`, `make`, `new`, `complex`, `real`, `imag`
  - `int`, `uint`, `any`, `complex64`, `complex128`, `uintptr`

## License

See LICENSE file for details.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
