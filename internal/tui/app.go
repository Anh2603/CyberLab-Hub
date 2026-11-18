package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type UI struct {
	application *tview.Application
	pages       *tview.Pages
	client      *Client
	ctx         context.Context
	cancel      context.CancelFunc

	status             *tview.TextView
	notifications      *tview.TextView
	events             chan string
	statuses           chan string
	roomCancel         context.CancelFunc
	roomSend           func(string) error
	progressList       *tview.List
	notificationCancel context.CancelFunc
	connecting         bool
}

func NewUI(client *Client) *UI {
	ctx, cancel := context.WithCancel(context.Background())
	ui := &UI{
		application: tview.NewApplication(), pages: tview.NewPages(), client: client,
		ctx: ctx, cancel: cancel, events: make(chan string, 32), statuses: make(chan string, 16),
		status:        tview.NewTextView().SetDynamicColors(true),
		notifications: tview.NewTextView().SetDynamicColors(true).SetScrollable(true),
	}
	ui.status.SetBorder(true).SetTitle(" Status ")
	ui.notifications.SetBorder(true).SetTitle(" Notifications ")
	ui.pages.AddPage("auth", ui.authScreen(), true, true)
	return ui
}

func (ui *UI) Run() error {
	go ui.consumeEvents()
	defer ui.cancel()
	return ui.application.SetRoot(ui.pages, true).EnableMouse(true).Run()
}

func (ui *UI) authScreen() tview.Primitive {
	return ui.accountScreen(false)
}

func (ui *UI) accountScreen(register bool) tview.Primitive {
	form := tview.NewForm()
	title := " CyberLab Hub - Sign in "
	hint := "Already have an account? Enter your details and choose Login.\nNew here? Choose Sign up to create an account."
	height := 12
	if register {
		title = " CyberLab Hub - Create account "
		hint = "Choose a username and a password of at least 8 characters.\nAlready registered? Choose Back to login."
		height = 14
	}
	form.SetBorder(true).SetTitle(title)
	form.AddInputField("Username", "", 32, nil, nil)
	form.AddPasswordField("Password", "", 32, '*', nil)
	if register {
		form.AddPasswordField("Confirm password", "", 32, '*', nil)
	}
	action := func() {
		if ui.connecting {
			return
		}
		username := form.GetFormItemByLabel("Username").(*tview.InputField).GetText()
		password := form.GetFormItemByLabel("Password").(*tview.InputField).GetText()
		if register {
			confirmation := form.GetFormItemByLabel("Confirm password").(*tview.InputField).GetText()
			if password != confirmation {
				ui.setStatus("[red]Passwords do not match[-]")
				return
			}
		}
		ui.connecting = true
		ui.setStatus("[yellow]Connecting...[-]")
		go func() {
			ctx, cancel := context.WithTimeout(ui.ctx, 6*time.Second)
			defer cancel()
			user, err := ui.client.Authenticate(ctx, username, password, register)
			ui.application.QueueUpdateDraw(func() {
				ui.connecting = false
				if err != nil {
					ui.setStatus("[red]" + tview.Escape(err.Error()) + "[-]")
					return
				}
				ui.setStatus(fmt.Sprintf("[green]Signed in as %s (%s)[-]", tview.Escape(user.Username), user.Role))
				ui.openMain()
				ui.pages.RemovePage("auth").RemovePage("signup")
				go ui.client.RunProgressSync(ui.ctx, ui.events, ui.statuses)
				ui.subscribeNotifications()
			})
		}()
	}
	if register {
		form.AddButton("Create account", action)
		form.AddButton("Back to login", func() {
			if ui.connecting {
				return
			}
			ui.setStatus("")
			ui.show("auth", ui.authScreen())
		})
	} else {
		form.AddButton("Login", action)
		form.AddButton("Sign up", func() {
			if ui.connecting {
				return
			}
			ui.setStatus("")
			ui.show("signup", ui.accountScreen(true))
		})
	}
	form.AddButton("Quit", ui.application.Stop)
	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().AddItem(nil, 0, 1, false).AddItem(form, 68, 1, true).AddItem(nil, 0, 1, false), height, 1, true).
		AddItem(tview.NewTextView().SetText(hint).SetTextAlign(tview.AlignCenter), 2, 0, false).
		AddItem(ui.status, 3, 0, false).
		AddItem(nil, 0, 1, false)
}

func (ui *UI) openMain() {
	if ui.roomCancel != nil {
		ui.roomCancel()
		ui.roomCancel = nil
		ui.roomSend = nil
	}
	menu := tview.NewList().ShowSecondaryText(true)
	user := ui.client.User()
	menu.SetBorder(true).SetTitle(fmt.Sprintf(" CyberLab Hub | %s (%s) ", tview.Escape(user.Username), user.Role))
	menu.AddItem("Challenges", "Browse and solve hands-on exercises", 'c', ui.openChallenges)
	menu.AddItem("My progress", "View persisted solved challenges", 'p', ui.openProgress)
	menu.AddItem("Discussion", "Join a live topic room", 'd', ui.openDiscussion)
	menu.AddItem("Notifications", "View UDP and TCP events", 'n', func() {
		ui.openNotifications()
	})
	if ui.client.User().Role == "admin" {
		menu.AddItem("Manage challenges", "Publish or archive catalog challenges", 'a', ui.openAdmin)
	}
	menu.AddItem("Quit", "Stop all network sessions", 'q', ui.application.Stop)
	ui.show("main", tview.NewFlex().SetDirection(tview.FlexRow).AddItem(menu, 0, 1, true).AddItem(ui.status, 3, 0, false))
}

func (ui *UI) openChallenges() {
	search := tview.NewInputField().SetLabel("Search: ").SetFieldWidth(30)
	list := tview.NewList().ShowSecondaryText(true)
	list.SetBorder(true).SetTitle(" Challenges ")
	load := func() {
		ui.setStatus("[yellow]Loading challenges...[-]")
		go func(query string) {
			ctx, cancel := context.WithTimeout(ui.ctx, 6*time.Second)
			defer cancel()
			challenges, err := ui.client.SearchChallenges(ctx, query)
			ui.application.QueueUpdateDraw(func() {
				list.Clear()
				if err != nil {
					ui.setStatus("[red]" + tview.Escape(err.Error()) + "[-]")
					return
				}
				for _, challenge := range challenges {
					selected := challenge
					list.AddItem(selected.Title, selected.Category+" · "+selected.Difficulty, 0, func() { ui.openChallenge(selected) })
				}
				ui.setStatus(fmt.Sprintf("[green]Loaded %d challenges[-]", len(challenges)))
			})
		}(search.GetText())
	}
	buttons := tview.NewForm().AddFormItem(search).AddButton("Load", load).AddButton("Back", ui.openMain)
	layout := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(buttons, 5, 0, true).AddItem(list, 0, 1, false).AddItem(ui.status, 3, 0, false)
	layout.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyF2:
			ui.application.SetFocus(list)
			return nil
		case tcell.KeyF3:
			ui.application.SetFocus(buttons)
			return nil
		case tcell.KeyEscape:
			ui.openMain()
			return nil
		}
		return event
	})
	list.SetTitle(" Challenges | F2: results | F3: search | Esc: menu ")
	ui.show("challenges", layout)
	load()
}

func (ui *UI) openChallenge(challenge Challenge) {
	details := tview.NewTextView().SetDynamicColors(true).SetWrap(true)
	details.SetBorder(true).SetTitle(" " + challenge.Title + " ")
	details.SetText(fmt.Sprintf("[yellow]%s · %s[-]\n\n%s", tview.Escape(challenge.Category), tview.Escape(challenge.Difficulty), tview.Escape(challenge.Description)))
	answer := tview.NewInputField().SetLabel("Answer: ").SetFieldWidth(50)
	form := tview.NewForm().AddFormItem(answer)
	form.AddButton("Submit", func() {
		go func(value string) {
			ctx, cancel := context.WithTimeout(ui.ctx, 6*time.Second)
			defer cancel()
			correct, err := ui.client.SubmitAnswer(ctx, challenge.ID, value)
			ui.application.QueueUpdateDraw(func() {
				if err != nil {
					ui.setStatus("[red]" + tview.Escape(err.Error()) + "[-]")
				} else if correct {
					ui.setStatus("[green]Correct — progress saved[-]")
				} else {
					ui.setStatus("[red]Incorrect answer[-]")
				}
			})
		}(answer.GetText())
	})
	form.AddButton("Back", ui.openChallenges)
	ui.show("challenge", ui.contentScreen(details, form, 5))
}

func (ui *UI) openProgress() {
	list := tview.NewList()
	list.SetBorder(true).SetTitle(" My progress ")
	ui.progressList = list
	ui.show("progress", ui.contentScreen(list, tview.NewForm().AddButton("Back", ui.openMain), 3))
	ui.refreshProgress()
}

func (ui *UI) refreshProgress() {
	list := ui.progressList
	if list == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(ui.ctx, 6*time.Second)
		defer cancel()
		progress, err := ui.client.Progress(ctx)
		ui.application.QueueUpdateDraw(func() {
			if err != nil {
				ui.setStatus("[red]" + tview.Escape(err.Error()) + "[-]")
				return
			}
			list.Clear()
			for _, item := range progress {
				list.AddItem(item.ChallengeID, item.SolvedAt.Local().Format(time.RFC822), 0, nil)
			}
			ui.setStatus(fmt.Sprintf("[green]%d solved challenges[-]", len(progress)))
		})
	}()
}

func (ui *UI) openDiscussion() {
	messages := tview.NewTextView().SetDynamicColors(true).SetScrollable(true)
	messages.SetBorder(true).SetTitle(" Discussion ")
	room := tview.NewInputField().SetLabel("Room: ").SetText("log-analysis").SetFieldWidth(24)
	body := tview.NewInputField().SetLabel("Message: ").SetFieldWidth(50)
	form := tview.NewForm().AddFormItem(room).AddFormItem(body)
	form.AddButton("Connect", func() {
		if ui.roomCancel != nil {
			ui.roomCancel()
		}
		ui.roomSend = nil
		roomCtx, cancel := context.WithCancel(ui.ctx)
		ui.roomCancel = cancel
		incoming := make(chan string, 32)
		roomName := room.GetText()
		go func() {
			send, err := ui.client.JoinRoom(roomCtx, roomName, incoming)
			ui.application.QueueUpdateDraw(func() {
				if roomCtx.Err() != nil {
					return
				}
				if err != nil {
					ui.setStatus("[red]" + tview.Escape(err.Error()) + "[-]")
					return
				}
				ui.roomSend = send
				ui.setStatus("[green]Discussion connected[-]")
			})
			if err != nil {
				cancel()
				return
			}
			for message := range incoming {
				value := message
				ui.application.QueueUpdateDraw(func() { fmt.Fprintln(messages, tview.Escape(value)) })
			}
		}()
	})
	form.AddButton("Send", func() {
		if ui.roomSend == nil {
			ui.setStatus("[yellow]Connect to a room first[-]")
			return
		}
		send := ui.roomSend
		message := body.GetText()
		body.SetText("")
		go func() {
			if err := send(message); err != nil {
				ui.application.QueueUpdateDraw(func() {
					ui.setStatus("[red]" + tview.Escape(err.Error()) + "[-]")
				})
			}
		}()
	})
	form.AddButton("Back", ui.openMain)
	ui.show("discussion", ui.contentScreen(messages, form, 8))
}

func (ui *UI) openAdmin() {
	form := tview.NewForm()
	for _, field := range []string{"ID", "Title", "Description", "Category", "Difficulty", "Answer"} {
		form.AddInputField(field, "", 80, nil, nil)
	}
	form.SetBorder(true).SetTitle(" Publish challenge ")
	form.AddButton("Publish", func() {
		value := func(label string) string { return form.GetFormItemByLabel(label).(*tview.InputField).GetText() }
		challenge := Challenge{ID: value("ID"), Title: value("Title"), Description: value("Description"), Category: value("Category"), Difficulty: value("Difficulty")}
		answer := value("Answer")
		go func() {
			ctx, cancel := context.WithTimeout(ui.ctx, 6*time.Second)
			defer cancel()
			err := ui.client.PublishChallenge(ctx, challenge, answer)
			ui.application.QueueUpdateDraw(func() {
				if err != nil {
					ui.setStatus("[red]" + tview.Escape(err.Error()) + "[-]")
				} else {
					ui.setStatus("[green]Challenge published[-]")
				}
			})
		}()
	})
	form.AddButton("Archive ID", func() {
		id := form.GetFormItemByLabel("ID").(*tview.InputField).GetText()
		go func() {
			ctx, cancel := context.WithTimeout(ui.ctx, 6*time.Second)
			defer cancel()
			err := ui.client.ArchiveChallenge(ctx, id)
			ui.application.QueueUpdateDraw(func() {
				if err != nil {
					ui.setStatus("[red]" + tview.Escape(err.Error()) + "[-]")
					return
				}
				ui.setStatus("[green]Challenge archived[-]")
			})
		}()
	})
	form.AddButton("Back", ui.openMain)
	ui.show("admin", tview.NewFlex().SetDirection(tview.FlexRow).AddItem(form, 0, 1, true).AddItem(ui.status, 3, 0, false))
}

func (ui *UI) contentScreen(content, controls tview.Primitive, controlHeight int) tview.Primitive {
	help := tview.NewTextView().SetText(" F2: content | F3: controls | Esc: menu | Tab/Shift+Tab: fields | Enter: select")
	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(content, 0, 1, false).AddItem(controls, controlHeight, 0, true).
		AddItem(ui.status, 3, 0, false).AddItem(help, 1, 0, false)
	layout.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyF2:
			ui.application.SetFocus(content)
			return nil
		case tcell.KeyF3:
			ui.application.SetFocus(controls)
			return nil
		case tcell.KeyEscape:
			ui.openMain()
			return nil
		}
		return event
	})
	return layout
}

func (ui *UI) subscribeNotifications() {
	if ui.notificationCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(ui.ctx)
	ui.notificationCancel = cancel
	go ui.client.RunNotifications(ctx, ui.events, ui.statuses)
}

func (ui *UI) openNotifications() {
	controls := tview.NewForm()
	controls.AddButton("Subscribe", func() {
		ui.subscribeNotifications()
	})
	controls.AddButton("Unsubscribe", func() {
		if ui.notificationCancel != nil {
			ui.notificationCancel()
			ui.notificationCancel = nil
		}
		ui.setStatus("[yellow]UDP unsubscribe requested[-]")
	})
	controls.AddButton("Back", ui.openMain)
	ui.show("notifications", ui.contentScreen(ui.notifications, controls, 3))
}

func (ui *UI) show(name string, primitive tview.Primitive) {
	if name == "admin" {
		primitive.(*tview.Flex).SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			if event.Key() == tcell.KeyEscape {
				ui.openMain()
				return nil
			}
			return event
		})
	}
	ui.pages.AddAndSwitchToPage(name, primitive, true)
	ui.application.SetFocus(primitive)
}

func (ui *UI) setStatus(message string) { ui.status.SetText(message) }

func (ui *UI) consumeEvents() {
	for {
		select {
		case event := <-ui.events:
			value := event
			ui.application.QueueUpdateDraw(func() { fmt.Fprintln(ui.notifications, tview.Escape(value)) })
			ui.application.QueueUpdateDraw(func() {
				name, _ := ui.pages.GetFrontPage()
				if name == "progress" && strings.HasPrefix(value, "Progress synchronized:") {
					ui.refreshProgress()
				}
			})
		case status := <-ui.statuses:
			value := status
			ui.application.QueueUpdateDraw(func() {
				ui.setStatus("[blue]" + tview.Escape(value) + "[-]")
				fmt.Fprintln(ui.notifications, tview.Escape(value))
			})
		case <-ui.ctx.Done():
			return
		}
	}
}
