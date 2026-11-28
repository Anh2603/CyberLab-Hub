package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func notificationControls(term *mangaTerminal) *tview.Form {
	_, page := term.ui.pages.GetFrontPage()
	return page.(*tview.Flex).GetItem(0).(*tview.Flex).GetItem(1).(*tview.Form)
}

func clickNotificationControl(term *mangaTerminal, button int) {
	notificationControls(term).GetButton(button).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), func(p tview.Primitive) { term.ui.app.SetFocus(p) })
}

func TestMangaUDPFailureAllowsSubscribeRetry(t *testing.T) {
	h := startMangaBackend(t)
	c := h.client(t, "udp-retry-reader", true)
	admin := h.client(t, "operator", false)
	c.udpAddr = "127.0.0.1:65536" // Deterministic ResolveUDPAddr failure; no dependency on ICMP.
	term := startMangaTerminal(t, c)
	term.do(func() { term.ui.notificationScreen(); clickNotificationControl(term, 0) })
	term.wait(func() bool { return strings.Contains(term.ui.status.GetText(true), "UDP address invalid") })
	term.wait(func() bool { return term.ui.notificationCancel == nil })
	term.do(func() { c.udpAddr = h.udp; clickNotificationControl(term, 0) })
	term.wait(func() bool { return strings.Contains(term.ui.status.GetText(true), "UDP notifications subscribed") })
	if err := admin.ReleaseChapter(context.Background(), "naruto", 701); err != nil {
		t.Fatal(err)
	}
	term.wait(func() bool { return strings.Contains(term.ui.alerts.GetText(true), "New chapter: Naruto") })
	term.do(func() { clickNotificationControl(term, 1) })
	term.wait(func() bool { return term.ui.notificationCancel == nil })
}

func TestMangaUDPResubscribeIgnoresOldCompletion(t *testing.T) {
	h := startMangaBackend(t)
	c := h.client(t, "udp-resubscribe-reader", true)
	admin := h.client(t, "operator", false)
	term := startMangaTerminal(t, c)
	var oldGeneration, newGeneration uint64
	term.do(func() {
		term.ui.notificationScreen()
		clickNotificationControl(term, 0)
		oldGeneration = term.ui.notificationGeneration
		clickNotificationControl(term, 1)
		clickNotificationControl(term, 0)
		newGeneration = term.ui.notificationGeneration
	})
	term.wait(func() bool { return strings.Contains(term.ui.status.GetText(true), "UDP notifications subscribed") })
	term.do(func() {
		before := term.ui.status.GetText(true)
		term.ui.applyNotificationNotice(notificationNotice{generation: oldGeneration, text: "old worker stopped", finished: true})
		term.ui.applyNotificationNotice(notificationNotice{generation: oldGeneration, text: "stale alert", event: true})
		if term.ui.notificationGeneration != newGeneration || term.ui.notificationCancel == nil || term.ui.status.GetText(true) != before {
			t.Error("old worker changed the new subscription")
		}
		if strings.Contains(term.ui.alerts.GetText(true), "stale alert") {
			t.Error("stale alert appeared")
		}
	})
	if err := admin.ReleaseChapter(context.Background(), "naruto", 701); err != nil {
		t.Fatal(err)
	}
	term.wait(func() bool { return strings.Contains(term.ui.alerts.GetText(true), "New chapter: Naruto") })
	term.do(func() {
		clickNotificationControl(term, 1)
		term.ui.applyNotificationNotice(notificationNotice{generation: newGeneration, text: "late subscribed acknowledgement"})
		if term.ui.notificationCancel != nil || !strings.Contains(term.ui.status.GetText(true), "inactive") {
			t.Error("late acknowledgement restored an unsubscribed session")
		}
	})
}

func TestMangaUDPRejectedSubscriptionCanRetry(t *testing.T) {
	h := startMangaBackend(t)
	c := h.client(t, "udp-auth-retry-reader", true)
	token, _ := c.credentials()
	c.mu.Lock()
	c.token = "invalid-token"
	c.mu.Unlock()
	term := startMangaTerminal(t, c)
	term.do(func() { term.ui.notificationScreen(); clickNotificationControl(term, 0) })
	term.wait(func() bool {
		return term.ui.notificationCancel == nil && strings.Contains(term.ui.status.GetText(true), "invalid token")
	})
	c.mu.Lock()
	c.token = token
	c.mu.Unlock()
	term.do(func() { clickNotificationControl(term, 0) })
	term.wait(func() bool { return strings.Contains(term.ui.status.GetText(true), "UDP notifications subscribed") })
}
