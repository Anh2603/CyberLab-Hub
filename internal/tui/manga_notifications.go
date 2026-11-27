package tui

import (
	"context"
	"fmt"
)

type notificationNotice struct {
	generation uint64
	text       string
	event      bool
	finished   bool
}

func (u *MangaUI) subscribeNotifications() {
	if u.notificationCancel != nil {
		return
	}
	u.notificationGeneration++
	generation := u.notificationGeneration
	ctx, cancel := context.WithCancel(u.ctx)
	u.notificationCancel = cancel
	u.status.SetText("UDP subscription requested; waiting for server acknowledgement")
	go u.runNotificationSubscription(ctx, generation)
}

func (u *MangaUI) unsubscribeNotifications() {
	// Invalidate queued messages and completion from the previous subscription.
	u.notificationGeneration++
	if u.notificationCancel != nil {
		u.notificationCancel()
		u.notificationCancel = nil
	}
	u.status.SetText("UDP notifications inactive; Subscribe to enable alerts")
}

// Only the tview event loop changes subscription state or widgets.
func (u *MangaUI) applyNotificationNotice(update notificationNotice) {
	if update.generation != u.notificationGeneration || u.notificationCancel == nil {
		return
	}
	if update.finished {
		u.notificationCancel()
		u.notificationCancel = nil
	}
	u.status.SetText(update.text)
	if update.event {
		fmt.Fprintln(u.alerts, update.text)
	}
}

func (u *MangaUI) runNotificationSubscription(ctx context.Context, generation uint64) {
	events, states := make(chan string, 64), make(chan string, 32)
	result := make(chan error, 1)
	go func() { result <- u.client.RunNotifications(ctx, events, states) }()
	deliver := func(update notificationNotice) bool {
		update.generation = generation
		select {
		case u.notificationUpdates <- update:
			return true
		case <-ctx.Done():
			return false
		}
	}
	for {
		select {
		case text := <-events:
			if !deliver(notificationNotice{text: text, event: true}) {
				return
			}
		case text := <-states:
			if !deliver(notificationNotice{text: text}) {
				return
			}
		case err := <-result:
			text := "UDP notifications inactive; Subscribe to retry"
			if err != nil {
				text += ": " + err.Error()
			}
			deliver(notificationNotice{text: text, finished: true})
			return
		case <-ctx.Done():
			return
		}
	}
}
