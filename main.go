package main

import (
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

var incomingMsgChan = make(chan MessagePayload, 100)
var deliveryResultChan = make(chan []DeliveryResult, 10)
var updateUIChan = make(chan struct{}, 10)

func main() {
	a := app.NewWithID("com.lanchat.app")

	cfg, err := LoadConfig()
	if err != nil {
		// Show setup window
		setupWin := a.NewWindow("LAN-CHAT Setup")
		
		usernameEntry := widget.NewEntry()
		usernameEntry.SetPlaceHolder("System Hostname or Username")
		
		pwdEntry := widget.NewPasswordEntry()
		pwdEntry.SetPlaceHolder("Room Password (leave blank for default)")
		
		historyCheck := widget.NewCheck("Enable message history saving?", nil)
		historyCheck.Checked = true

		form := &widget.Form{
			Items: []*widget.FormItem{
				{Text: "Username", Widget: usernameEntry},
				{Text: "Room Password", Widget: pwdEntry},
				{Text: "History", Widget: historyCheck},
			},
			OnSubmit: func() {
				u := strings.TrimSpace(usernameEntry.Text)
				u = strings.ReplaceAll(u, " ", "_")
				if u == "" {
					u = "Anonymous"
				}

				c := &Config{
					Username:     u,
					RoomPassword: pwdEntry.Text,
					HistoryOn:    historyCheck.Checked,
				}
				SaveConfig(c)
				setupWin.Close()
				startMainApp(a, c)
			},
		}

		setupWin.SetContent(container.NewVBox(
			widget.NewLabel("Welcome to LAN-CHAT!"),
			form,
		))
		setupWin.Resize(fyne.NewSize(400, 250))
		setupWin.Show()
	} else {
		startMainApp(a, cfg)
	}

	a.Run()
}

func startMainApp(a fyne.App, cfg *Config) {
	w := a.NewWindow("LAN-CHAT - " + cfg.Username)

	InitEncryption(cfg.RoomPassword)

	tcpPort, err := StartTCPServer(func(msg MessagePayload) {
		incomingMsgChan <- msg
	})
	if err != nil {
		dialog.ShowError(err, w)
		return
	}

	StartDiscovery(cfg.Username, tcpPort, updateUIChan)

	messagesData := binding.NewStringList()
	messageList := widget.NewListWithData(
		messagesData,
		func() fyne.CanvasObject { 
			lbl := widget.NewLabel("Template Message")
			lbl.Wrapping = fyne.TextWrapWord
			return lbl 
		},
		func(i binding.DataItem, obj fyne.CanvasObject) {
			str, _ := i.(binding.String).Get()
			obj.(*widget.Label).SetText(str)
		},
	)

	appendChat := func(msg string) {
		messagesData.Append(msg)
		messageList.ScrollToBottom()
	}

	activePeersData := binding.NewStringList()
	peerSidebar := widget.NewListWithData(
		activePeersData,
		func() fyne.CanvasObject { return widget.NewLabel("Peer") },
		func(i binding.DataItem, obj fyne.CanvasObject) {
			str, _ := i.(binding.String).Get()
			obj.(*widget.Label).SetText(str)
		},
	)

	peerSelect := widget.NewSelect([]string{"Send to All"}, nil)
	peerSelect.SetSelected("Send to All")

	updatePeers := func() {
		peers := GetActivePeers()
		
		options := []string{"Send to All"}
		var plist []string
		
		for ip, p := range peers {
			options = append(options, p.Username)
			plist = append(plist, fmt.Sprintf("%s\n%s", p.Username, ip))
		}
		
		validSelection := false
		for _, opt := range options {
			if opt == peerSelect.Selected {
				validSelection = true
				break
			}
		}
		if !validSelection {
			peerSelect.SetSelected("Send to All")
		}
		
		peerSelect.Options = options
		peerSelect.Refresh()
		
		activePeersData.Set(plist)
	}

	inputField := widget.NewEntry()
	inputField.SetPlaceHolder("Type your message here...")
	
	sendMsg := func() {
		text := strings.TrimSpace(inputField.Text)
		if text == "" {
			return
		}
		
		target := peerSelect.Selected
		targetUser := ""
		if target != "Send to All" {
			targetUser = target
		}
		
		ts := time.Now().Format("15:04")
		if targetUser != "" {
			appendChat(fmt.Sprintf("%s [%s -> %s]: %s", ts, cfg.Username, targetUser, text))
		} else {
			appendChat(fmt.Sprintf("%s [%s]: %s", ts, cfg.Username, text))
		}
		
		peers := GetActivePeers()
		if len(peers) > 0 {
			go SendMessages(peers, cfg.Username, text, targetUser, deliveryResultChan)
		}
		
		inputField.SetText("")
	}
	
	inputField.OnSubmitted = func(s string) { sendMsg() }
	sendBtn := widget.NewButtonWithIcon("Send", theme.MailSendIcon(), sendMsg)

	delBtn := widget.NewButtonWithIcon("Delete History", theme.DeleteIcon(), func() {
		messagesData.Set([]string{})
	})

	go func() {
		for {
			select {
			case <-updateUIChan:
				updatePeers()
			case msg := <-incomingMsgChan:
				ts := time.Unix(msg.Timestamp, 0).Format("15:04")
				formattedMsg := fmt.Sprintf("%s [%s]: %s", ts, msg.Sender, msg.Content)
				appendChat(formattedMsg)
				a.SendNotification(fyne.NewNotification("LAN-CHAT Msg", msg.Sender+": "+msg.Content))
			case results := <-deliveryResultChan:
				status := ""
				for _, res := range results {
					if res.Success {
						status += fmt.Sprintf("✅ %s ", res.Username)
					} else {
						status += fmt.Sprintf("❌ %s ", res.Username)
					}
				}
				appendChat(fmt.Sprintf("System | Delivery: %s", status))
			}
		}
	}()

	hour := time.Now().Hour()
	greeting := "Good evening"
	if hour >= 5 && hour < 12 {
		greeting = "Good morning"
	} else if hour >= 12 && hour < 17 {
		greeting = "Good afternoon"
	}
	appendChat(fmt.Sprintf("%s, %s! You are bound to port %d.\n", greeting, cfg.Username, tcpPort))

	bottom := container.NewBorder(nil, nil, nil, container.NewHBox(peerSelect, sendBtn, delBtn), inputField)
	mainContent := container.NewBorder(nil, bottom, nil, nil, messageList)
	
	split := container.NewHSplit(peerSidebar, mainContent)
	split.Offset = 0.2
	
	w.SetContent(split)
	w.Resize(fyne.NewSize(800, 600))
	w.Show()

	updateUIChan <- struct{}{}
}
