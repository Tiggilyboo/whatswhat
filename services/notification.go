package services

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/esiqveland/notify"
	"github.com/godbus/dbus/v5"
)

type Notification struct {
	ID            uint32
	Summary       string
	Body          string
	Icon          string
	ExpirySeconds uint8
}

type NotificationService struct {
	ctx                    context.Context
	notificationSendChan   chan *Notification
	notificationActionChan chan *Notification
	notificationsPending   sync.Map
	conn                   *dbus.Conn
	notifier               notify.Notifier
	notificationCounter    atomic.Uint32
}

func NewNotificationService(ctx context.Context) (*NotificationService, error) {
	conn, err := dbus.SessionBusPrivate()
	if err != nil {
		return nil, err
	}
	if err = conn.Auth(nil); err != nil {
		return nil, err
	}
	if err = conn.Hello(); err != nil {
		return nil, err
	}
	service := &NotificationService{
		ctx:                    ctx,
		conn:                   conn,
		notificationSendChan:   make(chan *Notification),
		notificationActionChan: make(chan *Notification),
	}
	notifier, err := notify.New(conn, notify.WithOnAction(service.handleNotificationAction))
	if err != nil {
		return nil, err
	}
	service.notifier = notifier

	// Cleanup everything when the passed context is stopped
	context.AfterFunc(ctx, service.Close)

	go service.consumeNotifications()

	return service, nil
}

func (n *NotificationService) QueueNotification(notification *Notification) {
	n.notificationSendChan <- notification
}

func (n *NotificationService) consumeNotifications() {
	for {
		select {
		case notification := <-n.notificationSendChan:
			n.sendNotification(notification)
		case <-n.ctx.Done():
			return
		}
	}
}

func (n *NotificationService) NextNotificationID() uint32 {
	return n.notificationCounter.Add(1)
}

func (n *NotificationService) sendNotification(notification *Notification) {
	notifyNotification := notify.Notification{
		AppName:    "WhatsWhat",
		ReplacesID: notification.ID,
		AppIcon:    notification.Icon,
		Summary:    notification.Summary,
		Body:       notification.Body,
		Actions: []notify.Action{
			{Key: "cancel", Label: "Cancel"},
			{Key: "open", Label: "Open"},
		},
		ExpireTimeout: notify.ExpireTimeoutNever,
	}
	id, err := n.notifier.SendNotification(notifyNotification)
	if err != nil {
		fmt.Printf("error sending notification: %s\n", err.Error())
		return
	}

	fmt.Printf("sent notification id: %v\n", id)
	n.notificationsPending.Store(notification.ID, notification)
}

func (n *NotificationService) Close() {
	n.conn.Close()
	n.notifier.Close()
	close(n.notificationActionChan)
	close(n.notificationSendChan)
}

func (n *NotificationService) handleNotificationAction(s *notify.ActionInvokedSignal) {
	fmt.Printf("Notification action invoked: %v Key: %v", s.ID, s.ActionKey)
	if sentNotification, ok := n.notificationsPending.Load(s.ID); ok {
		n.notificationActionChan <- sentNotification.(*Notification)
		n.notificationsPending.Delete(s.ID)
	}
}

func (n *NotificationService) handleNotificationClosed(s *notify.NotificationClosedSignal) {
	fmt.Printf("Notification close invoked: %v Reason: %v", s.ID, s.Reason)
	n.notificationsPending.Delete(s.ID)
}
