package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/gen2brain/beeep"
	"github.com/rivo/tview"
)

var app *tview.Application
var peerList *tview.TextView
var chatHistory *tview.TextView
var inputField *tview.InputField

var incomingMsgChan = make(chan MessagePayload, 100)
var deliveryResultChan = make(chan []DeliveryResult, 10)
var updateUIChan = make(chan struct{}, 10)

func setupFirstRun() *Config {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Welcome to LAN-CHAT!\n")
	fmt.Print("Enter Username (default: system hostname): ")
	user, _ := reader.ReadString('\n')
	user = strings.TrimSpace(user)
	user = strings.ReplaceAll(user, " ", "_")
	if user == "" {
		host, err := os.Hostname()
		if err == nil {
			user = strings.ReplaceAll(host, " ", "_")
		} else {
			user = "Anonymous"
		}
	}

	fmt.Print("Enable message history saving? [Y/n]: ")
	hist, _ := reader.ReadString('\n')
	hist = strings.TrimSpace(strings.ToLower(hist))
	historyOn := true
	if hist == "n" || hist == "no" {
		historyOn = false
	}

	fmt.Print("Enter Chat Room Password (leave blank for default): ")
	pwd, _ := reader.ReadString('\n')
	pwd = strings.TrimRight(pwd, "\r\n")

	cfg := &Config{
		Username:     user,
		HistoryOn:    historyOn,
		RoomPassword: pwd,
	}
	
	SaveConfig(cfg)
	return cfg
}

func main() {
	cfg, err := LoadConfig()
	if err != nil {
		// Try again... maybe it's the very first run
		cfg = setupFirstRun()
	}

	InitEncryption(cfg.RoomPassword)

	// TCP
	tcpPort, err := StartTCPServer(func(msg MessagePayload) {
		incomingMsgChan <- msg
	})
	if err != nil {
		fmt.Println("Failed to start TCP server:", err)
		return
	}

	// UDP
	StartDiscovery(cfg.Username, tcpPort, updateUIChan)
	// Trigger an initial UI update so we don't start blank
	updateUIChan <- struct{}{}

	app = tview.NewApplication()

	peerList = tview.NewTextView().
		SetDynamicColors(true).
		SetRegions(true).
		SetWordWrap(false)
	peerList.SetTitle(" 📡 Peers (0) ").SetBorder(true)

	chatHistory = tview.NewTextView().
		SetDynamicColors(true).
		SetRegions(true).
		SetWordWrap(true).
		SetScrollable(true).
		SetChangedFunc(func() {
			app.Draw()
		})
	chatHistory.SetTitle(" 💬 LAN-CHAT ").SetBorder(true)
	
	asciiLogo := `
    __    ___    _   __        ____ __  __ ___   _____
   / /   /   |  / | / /       / ___// / / /   | /_  _/
  / /   / /| | /  |/ / ______/ /   / /_/ / /| |  / /  
 / /___/ ___ |/ /|  / /_____/ /___/ __  / ___ | / /   
/_____/_/  |_/_/ |_/        \____/_/ /_/_/  |_|/_/    
                                                      
`
	fmt.Fprint(chatHistory, "[green]"+asciiLogo+"[white]\n")
	
	hour := time.Now().Hour()
	greeting := "Good evening"
	if hour >= 5 && hour < 12 {
		greeting = "Good morning"
	} else if hour >= 12 && hour < 17 {
		greeting = "Good afternoon"
	}

	fmt.Fprintf(chatHistory, "[yellow]%s, %s! You are bound to port %d.[white]\n\n", greeting, cfg.Username, tcpPort)

	inputField = tview.NewInputField().
		SetLabel(" > ").
		SetFieldWidth(0).
		SetDoneFunc(func(key tcell.Key) {
			if key == tcell.KeyEnter {
				text := inputField.GetText()
				text = strings.TrimSpace(text)
				if text == "" {
					return
				}
				
				targetUser := ""
				
				// Simple syntax for direct messages: /msg Username Hello
				if strings.HasPrefix(text, "/msg ") {
					parts := strings.SplitN(text, " ", 3)
					if len(parts) >= 3 {
						targetUser = parts[1]
						text = parts[2]
					}
				}

				// Echo to self
				ts := time.Now().Format("15:04")
				if targetUser != "" {
					fmt.Fprintf(chatHistory, "[lightblue]%s [white][%s -> %s]: %s\n", ts, cfg.Username, targetUser, text)
				} else {
					fmt.Fprintf(chatHistory, "[lightblue]%s [white][%s]: %s\n", ts, cfg.Username, text)
				}
				
				// Dispatch SendMessages
				peers := GetActivePeers()
				if len(peers) > 0 {
					go SendMessages(peers, cfg.Username, text, targetUser, deliveryResultChan)
				}
				
				inputField.SetText("")
			}
		})

	flex := tview.NewFlex().
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(chatHistory, 0, 1, false).
			AddItem(inputField, 1, 0, true), 0, 3, true).
		AddItem(peerList, 25, 0, false)

    // Handle incoming events in background
	go func() {
		for {
			select {
			case <-updateUIChan:
				app.QueueUpdateDraw(func() {
					peers := GetActivePeers()
					peerList.Clear()
					fmt.Fprintf(peerList, "[white]Active: %d\n\n", len(peers))
					for ip, p := range peers {
						fmt.Fprintf(peerList, "🟢 [green]%s[white]\n  [gray]%s\n\n", p.Username, ip)
					}
					peerList.SetTitle(fmt.Sprintf(" 📡 Peers (%d) ", len(peers)))
				})
			case msg := <-incomingMsgChan:
				app.QueueUpdateDraw(func() {
					ts := time.Unix(msg.Timestamp, 0).Format("15:04")
					fmt.Fprintf(chatHistory, "[yellow]%s [green][%s][white]: %s\n", ts, msg.Sender, msg.Content)
					
					// Send a desktop notification
					_ = beeep.Notify("LAN-CHAT Msg", msg.Sender+": "+msg.Content, "")
				})
			case results := <-deliveryResultChan:
				app.QueueUpdateDraw(func() {
					ts := time.Now().Format("15:04")
					statusLine := fmt.Sprintf("\n[gray]%s System | Delivery: ", ts)
					for _, res := range results {
						if res.Success {
							statusLine += fmt.Sprintf("✅ %s ", res.Username)
						} else {
							// Show a little X with username
							statusLine += fmt.Sprintf("❌ %s ", res.Username)
						}
					}
					fmt.Fprintf(chatHistory, "%s[white]\n", statusLine)
				})
			}
		}
	}()

	if err := app.SetRoot(flex, true).EnableMouse(true).Run(); err != nil {
		panic(err)
	}
}
