# FolderSynchronizer

**FolderSynchronizer** is a cross-platform file synchronization service with a browser-based management UI, a REST API, and optional Windows system-tray controls. It synchronizes one or more *pairs* of folders using efficient comparison strategies (mtime or SHA-256 hash), real-time change watching, and flexible schedules (interval, cron, or custom time windows). Safety-first file operations (atomic writes), filterable file selection, and robust retry/debounce logic keep syncs reliable even on busy systems.

After each successfully synchronized file, FolderSynchronizer can run **post-sync hooks** that you define per pair. Hooks are filterable by extension or glob and can either:
- **Call an HTTP webhook** (method, URL, headers, templated body) to notify downstream systems or trigger external automation; or
- **Execute a local command** (executable + args, working dir, env vars) to kick off scripts, indexers, or any CLI tool.
  Hook templates expose rich variables (e.g., `RelPath`, `Basename`, `SourcePath`, `TargetPath`, `Timestamp`) so payloads and commands can reference the specific file that changed. Built-in validation and guardrails protect against dangerous commands, and last hook status is tracked for visibility.

Key capabilities:
- **Sync engine**
    - Real-time **watcher** mode with fsnotify and debouncing; resilient copy with retries for transient file locks.
    - **Strategies:** `mtime` (size+time tolerance) and `hash` (SHA-256) to detect real changes.
    - **Filters:** include by extensions; exclude via doublestar glob patterns; optional mirror deletes.
    - **Atomic copies** via temp+rename to avoid partial files.
- **Scheduling**
    - Modes: *watcher*, fixed *interval*, *cron*, or *custom* windows (start/end times with sub-intervals); *manual* runs supported via API/UI.
- **Automation & UX**
    - **Hooks:** HTTP webhooks and local commands with templating, filtering, and retry/error reporting.
    - **Web UI + REST API** for full management: configure pairs, enable/disable, trigger “Sync All”, inspect status.
    - **Windows system tray** for quick actions: open UI, sync all, toggle pairs.
- **Ops-friendly**
    - JSON config with validation, structured rotating logs, sane defaults, and portable paths.

FolderSynchronizer is designed to be “set-and-forget”: define pairs, choose how and when they run, wire hooks to your pipelines or scripts—and let it keep your targets up to date.

## ✨ Features

### 🔄 Smart Synchronization
- **Multiple sync strategies**: Modification time or SHA256 hash comparison
- **Real-time file watching**: Instant sync on file changes using fsnotify
- **Bidirectional sync**: Optional mirror deletions
- **File filtering**: Include/exclude by extensions and glob patterns
- **Atomic operations**: Safe file copying with temporary files

### ⏰ Advanced Scheduling
- **File Watcher**: Real-time synchronization on file changes
- **Interval Scheduling**: Fixed time intervals (e.g., every 30 minutes)
- **Cron Expressions**: Advanced timing with full cron support
- **Custom Schedules**: Complex time windows and weekday patterns
- **Manual Mode**: On-demand synchronization only

### 🎯 Post-Sync Hooks
- **HTTP Webhooks**: REST API notifications with template support
- **Command Execution**: Run custom scripts/commands after sync
- **File Filtering**: Hook triggers based on file types or patterns
- **Template Variables**: Access to file path, timestamp, and more

### 🖥️ User Interface
- **Web-based UI**: Modern, responsive interface accessible via browser
- **System Tray**: Quick access and control (Windows)
- **RESTful API**: Programmatic access to all features
- **Live Status**: Real-time sync status and statistics

### 🔧 Enterprise Features
- **Configuration Management**: JSON-based configuration with validation
- **Comprehensive Logging**: Structured logging with rotation
- **Error Handling**: Robust retry mechanisms and error reporting
- **Security**: Command validation and safe execution
- **Performance**: Concurrent operations and optimized I/O

## 📥 Installation

### Pre-built Binaries

Download the latest release for your platform from the [Releases](https://github.com/yourusername/FolderSynchronizer/releases) page.

### Build from Source

**Requirements:**
- Go 1.21 or later
- Git

```bash
# Clone the repository
git clone https://github.com/yourusername/FolderSynchronizer.git
cd FolderSynchronizer

# Install dependencies
go mod download

# Build for your platform
go build -ldflags="-s -w" -o syncronizer ./cmd/syncronizer

# Build GUI version (Windows - no console window)
go build -ldflags="-s -w -H=windowsgui" -o syncronizer.exe ./cmd/syncronizer
```

### Cross-Platform Build

```bash
# Windows 64-bit
GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o bin/syncronizer-windows-amd64.exe ./cmd/syncronizer

# Linux 64-bit
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o bin/syncronizer-linux-amd64 ./cmd/syncronizer

# macOS 64-bit (Intel)
GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o bin/syncronizer-darwin-amd64 ./cmd/syncronizer

# macOS ARM64 (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o bin/syncronizer-darwin-arm64 ./cmd/syncronizer
```

## 🚀 Quick Start

### 1. First Run

```bash
# Start the application
./syncronizer

# Or specify custom listen address
./syncronizer -listen 0.0.0.0:8080

# Run without system tray (headless mode)
./syncronizer -no-tray
```

### 2. Access Web Interface

Open your browser and navigate to:
- Local access: http://127.0.0.1:8080
- Network access: http://your-ip:8080

### 3. Create Your First Sync Pair

1. Click **"New Pair"** button
2. Fill in the basic settings:
    - **ID**: Unique identifier (e.g., `documents-backup`)
    - **Description**: Human-readable description
    - **Source Directory**: Path to source folder
    - **Target Directory**: Path to destination folder
3. Configure filtering (optional):
    - **Include Extensions**: `.pdf,.doc,.docx` (leave empty for all files)
    - **Exclude Patterns**: `**/*.tmp,**/node_modules/**`
4. Set up scheduling:
    - **File Watcher**: Real-time sync (recommended)
    - **Interval**: Every N minutes/hours
    - **Cron**: Advanced scheduling
    - **Custom**: Specific time windows
5. Add hooks (optional):
    - **HTTP**: Webhook notifications
    - **Command**: Custom scripts
6. Click **"Save"**

## 📖 Configuration

### Configuration File

The application automatically creates a `config.json` file:

```json
{
  "listen": "127.0.0.1:8080",
  "pairs": [
    {
      "id": "documents-sync",
      "description": "Sync important documents",
      "enabled": true,
      "source": "C:\\Users\\john\\Documents",
      "target": "D:\\Backup\\Documents",
      "includeExtensions": [".pdf", ".docx", ".xlsx"],
      "excludeGlobs": ["**/*.tmp", "**/~*"],
      "mirrorDeletes": false,
      "syncStrategy": "mtime",
      "debounceMs": 500,
      "schedule": {
        "type": "watcher"
      },
      "hooks": []
    }
  ]
}
```

### Command Line Options

```bash
./syncronizer [options]

Options:
  -listen string
        HTTP listen address (default "127.0.0.1:8080")
  -config string
        Path to config.json file (optional)
  -no-tray
        Disable system tray icon
  -help
        Show help information
```

### Environment Variables

- `SYNCRONIZER_CONFIG`: Path to configuration file
- `SYNCRONIZER_LISTEN`: Listen address override
- `SYNCRONIZER_LOG_LEVEL`: Logging level (debug, info, warn, error)

## 📋 Usage Examples

### Basic File Sync

```json
{
  "id": "photo-backup",
  "description": "Backup photos to external drive",
  "source": "/home/user/Pictures",
  "target": "/media/backup/Pictures",
  "includeExtensions": [".jpg", ".png", ".raw"],
  "schedule": {
    "type": "interval",
    "interval": "1h"
  }
}
```

### Development Workflow

```json
{
  "id": "code-deploy",
  "description": "Deploy built artifacts",
  "source": "./dist",
  "target": "/var/www/html",
  "includeExtensions": [".html", ".css", ".js"],
  "schedule": {
    "type": "watcher"
  },
  "hooks": [
    {
      "http": {
        "method": "POST",
        "url": "http://deploy-server/webhook",
        "bodyTemplate": "{\"file\":\"{{.RelPath}}\",\"timestamp\":\"{{.Timestamp}}\"}"
      }
    }
  ]
}
```

### Scheduled Business Backup

```json
{
  "id": "business-backup",
  "description": "Daily business data backup",
  "source": "C:\\Business\\Data",
  "target": "\\\\nas\\backups\\daily",
  "excludeGlobs": ["**/*.log", "**/temp/**"],
  "mirrorDeletes": true,
  "schedule": {
    "type": "cron",
    "cronExpr": "0 0 2 * * *"
  }
}
```

### Custom Work Hours Sync

```json
{
  "schedule": {
    "type": "custom",
    "custom": {
      "weekDays": [1, 2, 3, 4, 5],
      "startTime": "08:00",
      "endTime": "18:00",
      "interval": "30m"
    }
  }
}
```

## 🔌 API Reference

### Pairs Management

```bash
# List all pairs
GET /api/pairs

# Create new pair
POST /api/pairs
Content-Type: application/json
{...pair configuration...}

# Update existing pair
PUT /api/pairs/{id}
Content-Type: application/json
{...updated configuration...}

# Delete pair
DELETE /api/pairs/{id}

# Get pair status
GET /api/pairs/{id}/status
```

### Pair Operations

```bash
# Start pair
POST /api/pairs/{id}/start

# Stop pair
POST /api/pairs/{id}/stop

# Trigger immediate sync
POST /api/pairs/{id}/sync

# Test hooks
POST /api/pairs/{id}/test-hook
```

### System Operations

```bash
# Sync all enabled pairs
POST /api/syncAll

# Get schedule examples
GET /api/schedules/examples

# Health check
GET /healthz
```

## 🛠️ Advanced Configuration

### Sync Strategies

**Modification Time (mtime)**
- Fast comparison using file modification time and size
- Good for most use cases
- 2-second tolerance for filesystem differences

**Hash (SHA256)**
- Byte-by-byte comparison using SHA256 hash
- Slower but 100% accurate
- Use for critical data or when timestamps are unreliable

### Hook Templates

Available template variables:
- `{{.RelPath}}`: Relative file path
- `{{.Basename}}`: File name only
- `{{.SourcePath}}`: Full source path
- `{{.TargetPath}}`: Full target path
- `{{.Timestamp}}`: Current timestamp (RFC3339)

### Cron Expression Examples

```bash
# Every 15 minutes
"0 */15 * * * *"

# Daily at 2 AM
"0 0 2 * * *"

# Workdays at 9 AM, 1 PM, 5 PM
"0 0 9,13,17 * * 1-5"

# Every 30 minutes during work hours (8-20) on weekdays
"0 */30 8-20 * * 1-5"

# First day of every month at midnight
"0 0 0 1 * *"
```

### Security Considerations

**Command Hooks Security**
- Dangerous commands are blocked (`rm`, `del`, `format`, etc.)
- Pattern detection for destructive operations
- Commands run with application privileges
- Use absolute paths for executables

**HTTP Hooks Security**
- No automatic credential inclusion
- HTTPS recommended for sensitive data
- Request timeouts and retry limits
- Response size limits

## 📁 Project Structure

```
FolderSynchronizer/
├── cmd/syncronizer/          # Main application entry point
│   ├── main.go              # Application bootstrap
│   └── assets/              # Embedded resources
├── internal/                # Private application code
│   ├── api/                 # HTTP server and REST API
│   ├── config/              # Configuration management
│   ├── core/                # Core synchronization logic
│   ├── scheduler/           # Task scheduling system
│   ├── tray/                # System tray integration
│   └── logging/             # Logging configuration
├── docs/                    # Documentation
├── scripts/                 # Build and deployment scripts
└── test/                    # Tests and test data
```

## 🔧 Development

### Prerequisites

- Go 1.21+
- Git
- Make (optional)

### Setup Development Environment

```bash
# Clone repository
git clone https://github.com/yourusername/FolderSynchronizer.git
cd FolderSynchronizer

# Install dependencies
go mod download

# Run in development mode
go run ./cmd/syncronizer -listen 127.0.0.1:8080

# Run tests
go test ./...

# Build for development
make build

# Build for all platforms
make build-all
```

### Adding New Features

1. **Core Logic**: Add to `internal/core/`
2. **API Endpoints**: Add to `internal/api/server.go`
3. **UI Components**: Update `internal/api/web/`
4. **Configuration**: Update `internal/config/config.go`
5. **Tests**: Add tests in corresponding `*_test.go` files

### Code Style

- Follow standard Go conventions
- Use `gofmt` for formatting
- Add comprehensive comments
- Include error handling
- Write tests for new features

## 🐛 Troubleshooting

### Common Issues

**Permission Denied**
- Run with administrator/root privileges if needed
- Check file/folder permissions
- Ensure target directory is writable

**File Locks (Windows)**
- Application uses retry logic for locked files
- Increase debounce time for rapidly changing files
- Consider excluding temporary files

**High CPU Usage**
- Reduce file watcher scope with exclude patterns
- Increase debounce time
- Use interval scheduling instead of file watching

**Memory Usage**
- Large files use streaming copy
- Hash calculation uses buffered reading
- Check exclude patterns for temporary files

### Logs and Debugging

**Log Locations**
- Windows: `%APPDATA%\GoFolderSync\logs\`
- Linux: `~/.config/gofoldersync/logs/`
- macOS: `~/Library/Application Support/gofoldersync/logs/`
- Portable: `./logs/` (next to executable)

**Log Levels**
- `ERROR`: Critical errors
- `WARN`: Warning conditions
- `INFO`: General information
- `DEBUG`: Detailed debugging

**Enable Debug Logging**
```bash
# Set environment variable
export SYNCRONIZER_LOG_LEVEL=debug
./syncronizer
```

### Performance Tuning

**Large File Sets**
- Use hash strategy only when necessary
- Optimize include/exclude patterns
- Consider multiple smaller sync pairs

**Network Drives**
- Increase timeout values
- Use interval scheduling
- Test connectivity before sync

**Resource Usage**
- Adjust copy worker count
- Monitor memory usage with large files
- Use SSD for better performance

## 📝 Changelog

### v1.0.0 (2025-08-18)
- Initial release
- Core synchronization engine
- Web-based management interface
- System tray integration (Windows)
- Advanced scheduling system
- HTTP and command hooks
- Cross-platform support

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- [fsnotify](https://github.com/fsnotify/fsnotify) - Cross-platform file system notifications
- [zerolog](https://github.com/rs/zerolog) - Fast and simple logging
- [systray](https://github.com/getlantern/systray) - System tray integration
- [cron](https://github.com/robfig/cron) - Cron expression parsing
- [doublestar](https://github.com/bmatcuk/doublestar) - Glob pattern matching

## 🔗 Links

- **Documentation**: [Wiki](https://github.com/yourusername/FolderSynchronizer/wiki)
- **Issues**: [GitHub Issues](https://github.com/yourusername/FolderSynchronizer/issues)
- **Discussions**: [GitHub Discussions](https://github.com/yourusername/FolderSynchronizer/discussions)
- **Releases**: [GitHub Releases](https://github.com/yourusername/FolderSynchronizer/releases)

---

**Made with ❤️ by [Your Name](https://github.com/yourusername)**