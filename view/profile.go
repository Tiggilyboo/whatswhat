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
	"github.com/diamondburned/gotk4/pkg/pango"
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
	defer cancel()

	v := ProfileUiView{
		ctx:    ctx,
		cancel: cancel,
		parent: parent,
	}
	v.Box = gtk.NewBox(gtk.OrientationVertical, 0)
	v.name = gtk.NewLabel("Loading")
	fontDesc := v.name.PangoContext().FontDescription()
	fontDesc.SetSize(18 * pango.SCALE)
	v.name.PangoContext().SetFontDescription(fontDesc)

	v.status = gtk.NewLabel("...")

	v.noProfile = gtk.NewImageFromIconName("avatar-default-symbolic")
	v.noProfile.SetIconSize(gtk.IconSizeLarge)

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
	pv.ctx, pv.cancel = context.WithCancel(context.Background())
	defer pv.cancel()

	client := pv.parent.GetChatClient()

	var id types.JID
	if msg.Payload == nil {
		id = client.Store.ID.ToNonAD()
	} else {
		switch msg.Payload.(type) {
		case types.JID:
			payloadId, ok := msg.Payload.(types.JID)
			if !ok {
				return errors.New("Error casting current user id")
			}
			id = payloadId
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
	if pv.cancel != nil {
		pv.cancel()
	}
}

func (pv *ProfileUiView) updateFromUserId(id types.JID) error {
	client := pv.parent.GetChatClient()
	fmt.Println("ProfileUiView.Update: GetUserInfo: ", id)
	users, err := client.GetUserInfo([]types.JID{id})
	if err != nil {
		return err
	}

	userInfo, ok := users[id]
	if !ok {
		return fmt.Errorf("Unable to find user profile for: %s\n", id)
	}

	fmt.Println("ProfileUiView.Update: GetContact: ", id)
	contact, err := client.Store.Contacts.GetContact(id)
	if err != nil {
		return err
	}
	fmt.Println("ProfileUiView.Update: Contact: ", contact.FullName)

	pv.name.SetLabel(contact.FullName)
	pv.noProfile.SetVisible(true)
	pv.profile.SetVisible(false)

	if len(userInfo.Status) > 0 {
		pv.status.SetLabel(userInfo.Status)
	} else {
		onWhatsAppQuery, err := client.IsOnWhatsApp([]string{id.User})
		if err != nil {
			return err
		}
		if len(onWhatsAppQuery) > 0 && onWhatsAppQuery[0].IsIn {
			pv.status.SetLabel("On WhatsApp")
		} else {
			pv.status.SetLabel("Not on WhatsApp")
		}
	}

	return pv.updateProfileImage(id)
}

func (pv *ProfileUiView) updateProfileImage(id types.JID) error {
	fmt.Println("ProfileUiView.updateProfileImage: id: ", id)
	client := pv.parent.GetChatClient()

	pictureParams := whatsmeow.GetProfilePictureParams{
		Preview:     true,
		IsCommunity: false,
	}
	fmt.Println("ProfileUiView.updateProfileImage: GetProfilePictureImage: ", pictureParams)
	pictureInfo, err := client.GetProfilePictureInfo(id, &pictureParams)
	if err != nil {
		if errors.Is(err, whatsmeow.ErrProfilePictureNotSet) {
			fmt.Println("ProfileUiView.updateProfileImage: User has no profile image")
			pv.profile.Clear()
			pv.profile.SetVisible(false)
			pv.noProfile.SetVisible(true)
			return nil
		}
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

	return nil
}
