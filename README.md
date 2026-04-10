# LAN-CHAT

LAN-CHAT is a serverless, peer-to-peer, terminal-based chat application written in Go. It operates exclusively on your local network (LAN) and does not require an active internet connection. All communications are verified and secured dynamically using AES-GCM encryption natively routed through standard sub-network broadcasts.

## 🧑‍💻 For End Users

### Quick Start
1. Download the latest `lan-chat` executable for your operating system.
2. Double click the executable, or open a terminal in the folder and type `./lan-chat` (or `.\lan-chat.exe` on Windows).
3. The first time you run it, the interface will prompt you to enter a **Username** and an optional **Chat Room Password**.
   > **Note:** If you want to chat with specific people on your network securely, make sure you all type the exact same Room Password! Unmatching passwords will silently isolate your chats.
4. **Firewall Access:** Your Operating System (Windows Defender, macOS Security) will likely warn you that `lan-chat` wants to communicate over your internal network. **You must accept/allow this permission**, otherwise you will not be able to see or chat with anyone.

### Features
- **P2P Encrypted Messaging:** All payloads (UDP registry pings and TCP messages) are natively encrypted via AES-GCM.
- **Private Messaging:** Type `/msg [Username] [Your message]` to send a private ping to a specific user. It filters network noise to deliver a targeted TCP payload specifically to their client.
- **Offline Capable:** If your internet WAN line goes down but your local WiFi router/switch is still powered on, LAN-CHAT keeps working flawlessly!
- **Desktop Notifications:** Stay updated when you get a message even if the terminal is fully minimized/unfocused using native OS notification popups.
- **Dynamic Timestamps:** Built-in greetings based on your local machine clock!

---

## 🛠️ For Developers

### Architecture
The project is built entirely on Go standard library networking `net` primitives alongside the `rivo/tview` terminal UI framework:
- **`main.go`**: Command-line bootstrapping, OS-native dependency injections (`beeep`), input validation, and asynchronous event loop UI mapping.
- **`discovery.go`**: Computes explicit subnet broadcast masks dynamically from local interfaces via `net.Interfaces()`, bounding past generic 2.4/5GHz router restrictions to handle persistent UDP heartbeat announcements and dead-peer registry pruning. 
- **`network.go`**: Owns the persistent encryption contexts and handles parallel `TCP` dispatch loops targeting port bindings sequentially upwards from `49153`.
- **`config.go`**: Abstract persistence layer utilizing `.lan-chat.json` within cross-platform `os.UserHomeDir`.

### Building Locally Requirements
- Go v1.24+

### Installation & Make Instructions
1. Clone the repository and navigate into the `lanchatapp` directory.
2. Fetch the UI and System dependencies:
   ```bash
   go mod tidy
   ```
3. Build the binary format of your choice to automatically generate the single cross-platform executables:
   ```bash
   go build -o lan-chat.exe     # Windows
   go build -o lan-chat         # Unix/macOS
   ```
