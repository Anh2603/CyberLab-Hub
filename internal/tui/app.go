package tui

import (
	"context"
	"fmt"
	"time"

	"github.com/rivo/tview"
)

type UI struct {
	application *tview.Application
	pages       *tview.Pages
	client      *Client
	ctx         context.Context
	cancel      context.CancelFunc

	status        *tview.TextView
	notifications *tview.TextView
	events        chan string
	statuses      chan string
	roomCancel    context.CancelFunc
	roomSend      func(string) error
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
	form := tview.NewForm()
	form.SetBorder(true).SetTitle(" CyberLab Hub - Sign in ")
	form.AddInputField("Username", "", 32, nil, nil)
	form.AddPasswordField("Password", "", 32, '*', nil)
	action := func(register bool) {
		username := form.GetFormItemByLabel("Username").(*tview.InputField).GetText()
		password := form.GetFormItemByLabel("Password").(*tview.InputField).GetText()
		ui.setStatus("[yellow]Connecting...[-]")
		go func() {
			ctx, cancel := context.WithTimeout(ui.ctx, 6*time.Second)
			defer cancel()
			user, err := ui.client.Authenticate(ctx, username, password, register)
			ui.application.QueueUpdateDraw(func() {
				if err != nil {
					ui.setStatus("[red]" + tview.Escape(err.Error()) + "[-]")
					return
				}
				ui.setStatus(fmt.Sprintf("[green]Signed in as %s (%s)[-]", tview.Escape(user.Username), user.Role))
				ui.openMain()
				go ui.client.RunProgressSync(ui.ctx, ui.events, ui.statuses)
				go ui.client.RunNotifications(ui.ctx, ui.events, ui.statuses)
			})
		}()
	}
	form.AddButton("Login", func() { action(false) })
	form.AddButton("Register", func() { action(true) })
	form.AddButton("Quit", ui.application.Stop)
	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().AddItem(nil, 0, 1, false).AddItem(form, 60, 1, true).AddItem(nil, 0, 1, false), 12, 1, true).
		AddItem(ui.status, 3, 0, false).
		AddItem(nil, 0, 1, false)
}

func (ui *UI) openMain() {
	menu := tview.NewList().ShowSecondaryText(true)
	menu.SetBorder(true).SetTitle(" CyberLab Hub ")
	menu.AddItem("Challenges", "Browse and solve hands-on exercises", 'c', ui.openChallenges)
	menu.AddItem("My progress", "View persisted solved challenges", 'p', ui.openProgress)
	menu.AddItem("Discussion", "Join a live topic room", 'd', ui.openDiscussion)
	menu.AddItem("Notifications", "View UDP and TCP events", 'n', func() {
		ui.show("notifications", ui.withBack(ui.notifications))
	})
	if ui.client.User().Role == "admin" {
		menu.AddItem("Publish challenge", "Administrator catalog action", 'a', ui.openAdmin)
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
	ui.show("challenges", tview.NewFlex().SetDirection(tview.FlexRow).AddItem(buttons, 3, 0, true).AddItem(list, 0, 1, false).AddItem(ui.status, 3, 0, false))
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
	ui.show("challenge", tview.NewFlex().SetDirection(tview.FlexRow).AddItem(details, 0, 1, false).AddItem(form, 5, 0, true).AddItem(ui.status, 3, 0, false))
}

func (ui *UI) openProgress() {
	list := tview.NewList()
	list.SetBorder(true).SetTitle(" My progress ")
	ui.show("progress", tview.NewFlex().SetDirection(tview.FlexRow).AddItem(list, 0, 1, true).AddItem(tview.NewForm().AddButton("Back", ui.openMain), 3, 0, false).AddItem(ui.status, 3, 0, false))
	go func() {
		ctx, cancel := context.WithTimeout(ui.ctx, 6*time.Second)
		defer cancel()
		progress, err := ui.client.Progress(ctx)
		ui.application.QueueUpdateDraw(func() {
			if err != nil {
				ui.setStatus("[red]" + tview.Escape(err.Error()) + "[-]")
				return
			}
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
		go func() {
			send, err := ui.client.JoinRoom(roomCtx, room.GetText(), incoming)
			ui.application.QueueUpdateDraw(func() {
				if err != nil {
					ui.setStatus("[red]" + tview.Escape(err.Error()) + "[-]")
					return
				}
				ui.roomSend = send
				ui.setStatus("[green]Discussion connected[-]")
			})
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
		if err := ui.roomSend(body.GetText()); err != nil {
			ui.setStatus("[red]" + tview.Escape(err.Error()) + "[-]")
		}
		body.SetText("")
	})
	form.AddButton("Back", ui.openMain)
	ui.show("discussion", tview.NewFlex().SetDirection(tview.FlexRow).AddItem(messages, 0, 1, false).AddItem(form, 8, 0, true).AddItem(ui.status, 3, 0, false))
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
	form.AddButton("Back", ui.openMain)
	ui.show("admin", tview.NewFlex().SetDirection(tview.FlexRow).AddItem(form, 0, 1, true).AddItem(ui.status, 3, 0, false))
}

func (ui *UI) withBack(content tview.Primitive) tview.Primitive {
	return tview.NewFlex().SetDirection(tview.FlexRow).AddItem(content, 0, 1, false).
		AddItem(tview.NewForm().AddButton("Back", ui.openMain), 3, 0, true).AddItem(ui.status, 3, 0, false)
}

func (ui *UI) show(name string, primitive tview.Primitive) {
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
		case status := <-ui.statuses:
			value := status
			ui.application.QueueUpdateDraw(func() { ui.setStatus("[blue]" + tview.Escape(value) + "[-]") })
		case <-ui.ctx.Done():
			return
		}
	}
}
