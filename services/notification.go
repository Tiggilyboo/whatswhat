package services

import (
	"context"
	"fmt"

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
	ctx              context.Context
	notificationChan chan *Notification
	conn             *dbus.Conn
	notifier         notify.Notifier
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
		ctx:              ctx,
		conn:             conn,
		notificationChan: make(chan *Notification),
	}
	notifier, err := notify.New(conn, notify.WithOnAction(service.handleNotificationAction))
	if err != nil {
		return nil, err
	}
	service.notifier = notifier

	// Cleanup everything when the passed context is stopped
	context.AfterFunc(ctx, service.stop)

	go service.consumeNotifications()

	return service, nil
}

func (n *NotificationService) QueueNotification(notification *Notification) {
	n.notificationChan <- notification
}

func (n *NotificationService) consumeNotifications() {
	for {
		select {
		case notification := <-n.notificationChan:
			n.sendNotification(notification)
		case <-n.ctx.Done():
			return
		}
	}
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
}

func (n *NotificationService) stop() {
	n.conn.Close()
	n.notifier.Close()
}

func (n *NotificationService) handleNotificationAction(s *notify.ActionInvokedSignal) {
	fmt.Printf("Notification action invoked: %v Key: %v", s.ID, s.ActionKey)
}
