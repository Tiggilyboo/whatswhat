package view

import (
	"context"
	"errors"
	"fmt"

	"io"
	"net/http"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

type ProfileUiView struct {
	*gtk.Box
	profile   *gtk.Image
	noProfile *gtk.Image
	name      *gtk.Label
	status    *gtk.Label

	parent UiParent
	ctx    context.Context
	cancel context.CancelFunc
}

func NewProfileUiView(parent UiParent) *ProfileUiView {
	ctx, cancel := context.WithCancel(context.Background())
	v := ProfileUiView{
		ctx:    ctx,
		cancel: cancel,
		parent: parent,
	}
	v.Box = gtk.NewBox(gtk.OrientationVertical, 0)
	v.name = gtk.NewLabel("Loading")
	v.status = gtk.NewLabel("...")

	v.noProfile = gtk.NewImageFromIconName("avatar-default-symbolic")

	v.profile = gtk.NewImage()
	v.profile.SetVisible(false)

	v.Box.Append(v.noProfile)
	v.Box.Append(v.profile)
	v.Box.Append(v.name)
	v.Box.Append(v.status)

	return &v
}

func (pv *ProfileUiView) Title() string {
	return "WhatsWhat - Profile"
}

func (pv *ProfileUiView) Update(msg *UiMessage) error {
	fmt.Println("ProfileUiView.Update: ", msg)

	if msg.Error != nil {
		return msg.Error
	}

	client := pv.parent.GetChatClient()

	var id *types.JID

	// View current user
	if msg.Payload == nil {
		id = client.Store.ID
	} else {
		switch msg.Payload.(type) {
		case types.JID:
			payloadId, ok := msg.Payload.(types.JID)
			if !ok {
				return errors.New("Error casting current user id")
			}
			id = &payloadId
		default:
			return errors.New("Unexpected UI message payload: ")
		}
	}

	return pv.updateFromUserId(id)
}

func (pv *ProfileUiView) Done() <-chan struct{} {
	return pv.ctx.Done()
}

func (pv *ProfileUiView) Close() {
	if pv.ctx.Done() != nil {
		pv.cancel()
	}
}

func (pv *ProfileUiView) updateFromUserId(id *types.JID) error {
	client := pv.parent.GetChatClient()
	fmt.Println("ProfileUiView.Update: GetUserInfo: ", *id)
	users, err := client.GetUserInfo([]types.JID{*id})
	if err != nil {
		return err
	}

	userInfo, ok := users[*id]
	if !ok {
		return errors.New(fmt.Sprint("Unable to find user profile for: ", id.User))
	}

	fmt.Println("ProfileUiView.Update: GetContact: ", *id)
	contact, err := client.Store.Contacts.GetContact(*id)
	if err != nil {
		return err
	}
	fmt.Println("ProfileUiView.Update: Contact: ", contact.FullName)

	pv.name.SetLabel(contact.FullName)
	pv.status.SetLabel(userInfo.Status)
	pv.noProfile.SetVisible(true)
	pv.profile.SetVisible(false)

	return pv.updateProfileImage(id)
}

func (pv *ProfileUiView) updateProfileImage(id *types.JID) error {
	fmt.Println("ProfileUiView.updateProfileImage: id: ", id)
	client := pv.parent.GetChatClient()

	pictureParams := whatsmeow.GetProfilePictureParams{
		Preview:     true,
		IsCommunity: false,
	}
	fmt.Println("ProfileUiView.updateProfileImage: GetProfilePictureImage: ", pictureParams)
	pictureInfo, err := client.GetProfilePictureInfo(*id, &pictureParams)
	if err != nil {
		return err
	}

	fmt.Println("ProfileUiView.updateProfileImage: Get profile image: ", pictureInfo.URL)
	pictureResp, err := http.NewRequestWithContext(pv.ctx, "GET", pictureInfo.URL, nil)
	if err != nil {
		return err
	}
	defer pictureResp.Body.Close()

	pictureBytes, err := io.ReadAll(pictureResp.Body)
	if err != nil {
		return err
	}

	pictureGlibBytes := glib.NewBytes(pictureBytes)
	pictureTexture, err := gdk.NewTextureFromBytes(pictureGlibBytes)
	if err != nil {
		return err
	}

	fmt.Println("ProfileUiView.updateProfileImage: Updating profile from texture")
	pv.Box.Remove(pv.profile)
	pv.profile = gtk.NewImageFromPaintable(pictureTexture)
	pv.Box.Prepend(pv.profile)

	<-pv.ctx.Done()

	return nil
}
