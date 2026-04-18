# Building the Moxie IntelliJ Plugin

## Prerequisites

1. **JDK 17 or later**
   ```bash
   java -version
   ```

2. **IntelliJ IDEA** (for development, optional)
   - Community or Ultimate Edition 2023.2 or later

## Build Commands

### Build the plugin

```bash
./gradlew buildPlugin
```

The plugin will be generated in `build/distributions/moxie-intellij-plugin-1.0.0.zip`.

### Run in development mode

```bash
./gradlew runIde
```

This launches a new IntelliJ IDEA instance with the plugin loaded for testing.

### Verify the plugin

```bash
./gradlew verifyPlugin
```

Checks the plugin for compatibility issues.

### Clean build artifacts

```bash
./gradlew clean
```

## Installation

### From built ZIP file

1. Build the plugin: `./gradlew buildPlugin`
2. Open IntelliJ IDEA
3. Go to **Settings → Plugins**
4. Click the gear icon → **Install Plugin from Disk...**
5. Select `build/distributions/moxie-intellij-plugin-1.0.0.zip`
6. Restart IntelliJ IDEA

### Manual installation (development)

1. Open this project in IntelliJ IDEA
2. Run the `runIde` Gradle task
3. A new IDE instance will open with the plugin loaded

## Testing the Plugin

1. Create a new file with `.mx` extension
2. The file should be recognized as a Moxie file (see icon in file tab)
3. Copy the contents from `example.mx` to test syntax highlighting
4. Verify the following features:
   - Syntax highlighting for keywords, types, functions
   - Line comments (`//`) and block comments (`/* */`)
   - Brace matching
   - Code folding
   - Comment/uncomment actions (Ctrl+/ or Cmd+/)

## Customizing Colors

After installation:
1. Go to **Settings → Editor → Color Scheme → Moxie**
2. Customize the colors for different syntax elements
3. Changes will be applied immediately to open .mx files

## Project Structure

```
moxie-intellij-plugin/
├── build.gradle.kts              # Build configuration
├── settings.gradle.kts           # Project settings
├── gradle.properties             # Plugin properties
├── src/
│   └── main/
│       ├── kotlin/
│       │   └── com/moxie/lang/   # Plugin source code
│       └── resources/
│           ├── META-INF/
│           │   └── plugin.xml    # Plugin descriptor
│           └── icons/            # File icons
├── example.mx                    # Example Moxie file
├── README.md                     # User documentation
└── BUILDING.md                   # This file
```

## Troubleshooting

### Build fails with Java version error

Ensure you're using JDK 17 or later:
```bash
export JAVA_HOME=/path/to/jdk-17
./gradlew buildPlugin
```

### Plugin doesn't load in IntelliJ

1. Check the IDE version (must be 2023.2 or later)
2. Verify the plugin built successfully
3. Check the IDE log: **Help → Show Log in Finder/Explorer**

### Syntax highlighting doesn't work

1. Verify the file extension is `.mx`
2. Restart IntelliJ IDEA after installation
3. Check **Settings → Editor → File Types** to ensure `.mx` is associated with Moxie

## Publishing (for maintainers)

To publish to the JetBrains Plugin Marketplace:

1. Set the plugin repository credentials:
   ```bash
   export PUBLISH_TOKEN=your-token
   ```

2. Run the publish task:
   ```bash
   ./gradlew publishPlugin
   ```

## Development Workflow

1. Make changes to Kotlin source files
2. Run `./gradlew runIde` to test
3. Iterate and test
4. Build final plugin with `./gradlew buildPlugin`
5. Install and verify in production IntelliJ

## Additional Resources

- [IntelliJ Platform SDK](https://plugins.jetbrains.com/docs/intellij/welcome.html)
- [Gradle IntelliJ Plugin](https://github.com/JetBrains/gradle-intellij-plugin)
- [Syntax Highlighting Guide](https://plugins.jetbrains.com/docs/intellij/syntax-highlighting-and-error-highlighting.html)
