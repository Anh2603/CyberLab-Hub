package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Anh2603/CyberLab-Hub/internal/store"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Network goroutines dispatch widget mutations through tview's event loop.
type MangaUI struct {
	app                *tview.Application
	pages              *tview.Pages
	client             *Client
	ctx                context.Context
	cancel             context.CancelFunc
	status             *tview.TextView
	alerts             *tview.TextView
	events             chan string
	statuses           chan string
	library            *tview.List
	libraryVisible     bool
	notificationCancel context.CancelFunc
	roomCancel         context.CancelFunc
	busy               bool
	revision           uint64
}

func NewMangaUI(client *Client) *MangaUI {
	ctx, cancel := context.WithCancel(context.Background())
	u := &MangaUI{app: tview.NewApplication(), pages: tview.NewPages(), client: client, ctx: ctx, cancel: cancel, status: tview.NewTextView(), alerts: tview.NewTextView().SetScrollable(true), events: make(chan string, 64), statuses: make(chan string, 32)}
	u.status.SetBorder(true).SetTitle(" Connection / action status ")
	u.alerts.SetBorder(true).SetTitle(" UDP chapter notifications ")
	u.pages.AddPage("login", u.auth(false), true, true)
	return u
}
func (u *MangaUI) Run() error {
	go func() {
		for {
			select {
			case event := <-u.events:
				u.app.QueueUpdateDraw(func() {
					u.status.SetText(event)
					if strings.HasPrefix(event, "Reading progress:") && u.libraryVisible {
						u.loadLibrary()
					}
					if strings.HasPrefix(event, "New chapter:") {
						fmt.Fprintln(u.alerts, event)
					}
				})
			case state := <-u.statuses:
				u.app.QueueUpdateDraw(func() { u.status.SetText(state) })
			case <-u.ctx.Done():
				return
			}
		}
	}()
	defer u.cancel()
	return u.app.SetRoot(u.pages, true).EnableMouse(true).Run()
}
func (u *MangaUI) task(work func() error, done func()) {
	revision := u.revision
	go func() {
		err := work()
		u.app.QueueUpdateDraw(func() {
			if u.revision != revision {
				return
			}
			if err != nil {
				u.status.SetText("Error: " + err.Error())
				return
			}
			if done != nil {
				done()
			}
		})
	}()
}
func (u *MangaUI) show(name string, body tview.Primitive) {
	u.revision++
	u.libraryVisible = name == "library"
	if u.roomCancel != nil {
		u.roomCancel()
		u.roomCancel = nil
	}
	for _, p := range []string{"login", "main", "browser", "details", "library", "discussion", "notifications", "release", "help"} {
		u.pages.RemovePage(p)
	}
	u.pages.AddPage(name, body, true, true)
	u.app.SetFocus(body)
}
func (u *MangaUI) wrap(body tview.Primitive) tview.Primitive {
	flex := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(body, 0, 1, true).AddItem(u.status, 3, 0, false)
	flex.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		if e.Key() == tcell.KeyEscape {
			u.main()
			return nil
		}
		return e
	})
	return flex
}
func (u *MangaUI) auth(register bool) tview.Primitive {
	form := tview.NewForm()
	form.SetBorder(true).SetTitle(" MangaHub — Sign in ")
	form.AddInputField("Username", "", 32, nil, nil).AddPasswordField("Password", "", 32, '*', nil)
	if register {
		form.SetTitle(" MangaHub — Create account ")
		form.AddPasswordField("Confirm password", "", 32, '*', nil)
	}
	action := func() {
		if u.busy {
			return
		}
		username := form.GetFormItemByLabel("Username").(*tview.InputField).GetText()
		password := form.GetFormItemByLabel("Password").(*tview.InputField).GetText()
		if register && password != form.GetFormItemByLabel("Confirm password").(*tview.InputField).GetText() {
			u.status.SetText("Passwords do not match")
			return
		}
		u.busy = true
		u.status.SetText("Connecting…")
		go func() {
			_, err := u.client.Authenticate(u.ctx, username, password, register)
			u.app.QueueUpdateDraw(func() {
				u.busy = false
				if err != nil {
					u.status.SetText(err.Error())
					return
				}
				u.main()
				go u.client.RunProgressSync(u.ctx, u.events, u.statuses)
			})
		}()
	}
	if register {
		form.AddButton("Create account", action).AddButton("Back to login", func() { u.show("login", u.auth(false)) })
	} else {
		form.AddButton("Log in", action).AddButton("Sign up", func() { u.show("login", u.auth(true)) })
	}
	form.AddButton("Quit", u.app.Stop)
	return tview.NewFlex().SetDirection(tview.FlexRow).AddItem(nil, 0, 1, false).AddItem(tview.NewFlex().AddItem(nil, 0, 1, false).AddItem(form, 68, 1, true).AddItem(nil, 0, 1, false), 13, 0, true).AddItem(u.status, 3, 0, false).AddItem(nil, 0, 1, false)
}
func (u *MangaUI) main() {
	list := tview.NewList().ShowSecondaryText(true)
	list.SetBorder(true).SetTitle(" MangaHub — " + u.client.User().Username + " ")
	list.AddItem("Manga Browser", "Search catalog through HTTP → gRPC", 'b', u.browser)
	list.AddItem("My Library", "Reading status and chapter progress; automatic TCP updates", 'l', u.libraryScreen)
	list.AddItem("Discussion", "Join a room and exchange live messages", 'd', u.discussion)
	list.AddItem("Notifications", "Subscribe / unsubscribe to UDP chapter alerts", 'n', u.notificationScreen)
	if u.client.User().Role == "admin" {
		list.AddItem("Release chapter", "Update catalog through gRPC and notify UDP subscribers", 'a', u.release)
	}
	list.AddItem("Connection Status / Help", "Ports, keyboard controls, protocol limitations", 'h', u.help).AddItem("Quit", "Close this TUI session", 'q', u.app.Stop)
	u.show("main", u.wrap(list))
}
func (u *MangaUI) browser() {
	results := tview.NewList().ShowSecondaryText(true)
	results.SetBorder(true).SetTitle(" Select manga — Enter for details ")
	search := tview.NewInputField().SetLabel("Search title: ")
	load := func() {
		query := search.GetText()
		u.status.SetText("Loading catalog…")
		var items []store.Manga
		u.task(func() error { var err error; items, err = u.client.SearchManga(u.ctx, query); return err }, func() {
			results.Clear()
			for _, m := range items {
				m := m
				results.AddItem(m.Title, fmt.Sprintf("%s | %s | %d chapters", m.Author, strings.Join(m.Genres, ", "), m.TotalChapters), 0, func() { u.details(m.ID) })
			}
			u.status.SetText(fmt.Sprintf("%d manga found. F2 results, F3 search, Esc menu", len(items)))
			u.app.SetFocus(results)
		})
	}
	search.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			load()
		}
	})
	layout := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(search, 3, 0, true).AddItem(results, 0, 1, false)
	layout.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		if e.Key() == tcell.KeyF2 {
			u.app.SetFocus(results)
			return nil
		}
		if e.Key() == tcell.KeyF3 {
			u.app.SetFocus(search)
			return nil
		}
		return e
	})
	u.show("browser", u.wrap(layout))
	load()
}
func (u *MangaUI) details(id string) {
	var m store.Manga
	u.task(func() error { var err error; m, err = u.client.GetManga(u.ctx, id); return err }, func() {
		text := tview.NewTextView().SetText(fmt.Sprintf("%s\nAuthor: %s\nGenres: %s\nStatus: %s\nTotal chapters: %d\n\n%s", m.Title, m.Author, strings.Join(m.Genres, ", "), m.Status, m.TotalChapters, m.Description))
		text.SetBorder(true).SetTitle(" Manga details ")
		form := tview.NewForm().AddButton("Add to Library", func() {
			u.task(func() error { return u.client.AddManga(u.ctx, id) }, func() { u.status.SetText("Added to your library") })
		}).AddButton("Back", u.browser)
		u.show("details", u.wrap(tview.NewFlex().SetDirection(tview.FlexRow).AddItem(text, 0, 1, false).AddItem(form, 5, 0, true)))
	})
}
func (u *MangaUI) libraryScreen() {
	u.library = tview.NewList().ShowSecondaryText(true)
	u.library.SetBorder(true).SetTitle(" My Library — Enter to update, Esc menu ")
	u.show("library", u.wrap(u.library))
	u.loadLibrary()
}
func (u *MangaUI) loadLibrary() {
	list := u.library
	var entries []store.LibraryEntry
	u.task(func() error { var err error; entries, err = u.client.Library(u.ctx); return err }, func() {
		if !u.libraryVisible || u.library != list {
			return
		}
		list.Clear()
		for _, e := range entries {
			e := e
			list.AddItem(e.MangaID, fmt.Sprintf("%s | chapter %d | version %d", e.Status, e.CurrentChapter, e.Version), 0, func() { u.editProgress(e) })
		}
		if len(entries) == 0 {
			list.AddItem("Your library is empty", "Add manga from Manga Browser", 0, nil)
		}
		u.status.SetText("Library loaded. TCP updates appear without refresh.")
	})
}
func (u *MangaUI) editProgress(entry store.LibraryEntry) {
	var m store.Manga
	u.task(func() error { var err error; m, err = u.client.GetManga(u.ctx, entry.MangaID); return err }, func() {
		form := tview.NewForm()
		form.SetBorder(true).SetTitle(" Update: " + m.Title + " ")
		states := []string{"plan-to-read", "reading", "completed"}
		selected := 0
		for i, s := range states {
			if s == entry.Status {
				selected = i
			}
		}
		form.AddDropDown("Status", states, selected, nil).AddInputField("Current chapter", strconv.Itoa(entry.CurrentChapter), 12, tview.InputFieldInteger, nil)
		form.AddButton("Save", func() {
			chapter, err := strconv.Atoi(form.GetFormItemByLabel("Current chapter").(*tview.InputField).GetText())
			if err != nil || chapter < 0 || chapter > m.TotalChapters {
				u.status.SetText(fmt.Sprintf("Chapter must be between 0 and %d", m.TotalChapters))
				return
			}
			_, state := form.GetFormItemByLabel("Status").(*tview.DropDown).GetCurrentOption()
			u.task(func() error { _, err := u.client.UpdateReading(u.ctx, m.ID, state, chapter); return err }, u.libraryScreen)
		}).AddButton("Cancel", u.libraryScreen)
		u.show("details", u.wrap(form))
		u.status.SetText(fmt.Sprintf("Total chapters: %d. Server validates again before saving.", m.TotalChapters))
	})
}
func (u *MangaUI) notificationScreen() {
	form := tview.NewForm().AddButton("Subscribe", func() {
		if u.notificationCancel != nil {
			return
		}
		ctx, cancel := context.WithCancel(u.ctx)
		u.notificationCancel = cancel
		go u.client.RunNotifications(ctx, u.events, u.statuses)
	}).AddButton("Unsubscribe", func() {
		if u.notificationCancel != nil {
			u.notificationCancel()
			u.notificationCancel = nil
		}
	}).AddButton("Back", u.main)
	u.show("notifications", u.wrap(tview.NewFlex().SetDirection(tview.FlexRow).AddItem(u.alerts, 0, 1, false).AddItem(form, 5, 0, true)))
	u.status.SetText("UDP is best-effort. Subscription renews while this TUI is open.")
}
func (u *MangaUI) discussion() {
	room := tview.NewInputField().SetLabel("Room (F4): ").SetText("general")
	log := tview.NewTextView().SetScrollable(true)
	log.SetBorder(true).SetTitle(" Discussion messages (local date/time) ")
	input := tview.NewInputField().SetLabel("Message: ")
	var send func(string) error
	controls := tview.NewForm().AddButton("Join", func() {
		if u.roomCancel != nil {
			u.roomCancel()
		}
		roomCtx, roomCancel := context.WithCancel(u.ctx)
		u.roomCancel = roomCancel
		events := make(chan string, 32)
		name := room.GetText()
		send = nil
		var nextSend func(string) error
		u.task(func() error {
			var err error
			nextSend, err = u.client.JoinRoom(roomCtx, name, events)
			if err != nil {
				close(events)
			}
			return err
		}, func() {
			send = nextSend
			u.status.SetText("Connected to " + name + ". Enter sends; F2 message, F3 controls, F4 room.")
			u.app.SetFocus(input)
		})
		go func() {
			for message := range events {
				message := message
				u.app.QueueUpdateDraw(func() { fmt.Fprintln(log, message); log.ScrollToEnd() })
			}
		}()
	}).AddButton("Back", u.main)
	room.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			u.app.SetFocus(controls)
		}
	})
	input.SetDoneFunc(func(key tcell.Key) {
		if key != tcell.KeyEnter {
			return
		}
		text := input.GetText()
		if strings.TrimSpace(text) == "" {
			return
		}
		if send == nil {
			u.status.SetText("Join a room first")
			return
		}
		input.SetText("")
		sender := send
		u.task(func() error { return sender(text) }, nil)
	})
	layout := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(room, 2, 0, false).AddItem(controls, 5, 0, true).AddItem(log, 0, 1, false).AddItem(input, 3, 0, false)
	layout.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		if e.Key() == tcell.KeyF4 {
			u.app.SetFocus(room)
			return nil
		}
		if e.Key() == tcell.KeyF2 {
			u.app.SetFocus(input)
			return nil
		}
		if e.Key() == tcell.KeyF3 {
			u.app.SetFocus(controls)
			return nil
		}
		return e
	})
	u.show("discussion", u.wrap(layout))
}
func (u *MangaUI) release() {
	form := tview.NewForm()
	form.SetBorder(true).SetTitle(" Release new chapter (admin) ")
	form.AddInputField("Manga ID", "star-harbor", 64, nil, nil).AddInputField("New total chapters", "13", 10, tview.InputFieldInteger, nil)
	form.AddButton("Release", func() {
		id := form.GetFormItemByLabel("Manga ID").(*tview.InputField).GetText()
		total, err := strconv.Atoi(form.GetFormItemByLabel("New total chapters").(*tview.InputField).GetText())
		if err != nil {
			u.status.SetText("Enter an integer chapter count")
			return
		}
		u.task(func() error { return u.client.ReleaseChapter(u.ctx, id, total) }, func() { u.status.SetText("Catalog updated through gRPC; UDP alert sent best-effort") })
	}).AddButton("Back", u.main)
	u.show("release", u.wrap(form))
}
func (u *MangaUI) help() {
	text := tview.NewTextView().SetText(fmt.Sprintf("MangaHub\n\nSigned in: %s\nHTTP: %s\nTCP: %s\nUDP: %s\n\nArrows/Enter select; Tab changes form controls; Esc returns to menu.\nBrowser: F2 results / F3 search. Discussion: F2 message / F3 controls.\n\nKeep My Library open in another TUI under the same account for TCP updates.\nNotifications need a UDP subscription. UDP may be lost.\nChat is live and room-isolated; no persisted history.\nCatalog is a separate process; failures produce bounded errors.\n\nEsc returns to menu.", u.client.User().Username, u.client.baseURL, u.client.tcpAddr, u.client.udpAddr))
	u.show("help", u.wrap(text))
}
