package view

import (
	"context"
	"errors"
	"fmt"
	"time"

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
	profile    *gtk.Image
	noProfile  *gtk.Image
	name       *gtk.Label
	status     *gtk.Label
	updateChan chan *profileUiViewUpdate

	parent UiParent
	ctx    context.Context
	cancel context.CancelFunc
}

type profileUiViewUpdate struct {
	userJID           types.JID
	name              string
	status            string
	profileImageBytes *[]byte
}

func NewProfileUiView(parent UiParent) *ProfileUiView {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	v := ProfileUiView{
		ctx:        ctx,
		cancel:     cancel,
		parent:     parent,
		updateChan: make(chan *profileUiViewUpdate),
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

	go v.consumeProfileUpdates()

	return &v
}

func (pv *ProfileUiView) Title() string {
	return "WhatsWhat - Profile"
}

func (pv *ProfileUiView) Update(msg *UiMessage) error {
	fmt.Println("ProfileUiView.Update: ", msg)

	// Clear any old state
	pv.name.SetLabel("...")
	pv.status.SetLabel("...")
	pv.noProfile.SetVisible(true)
	pv.profile.Clear()
	pv.profile.SetVisible(false)

	if msg.Error != nil {
		return msg.Error
	}
	pv.ctx, pv.cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer pv.cancel()

	client := pv.parent.GetChatClient()

	var userJID types.JID
	if msg.Payload == nil {
		userJID = client.Store.ID.ToNonAD()
	} else {
		switch msg.Payload.(type) {
		case types.JID:
			payloadId, ok := msg.Payload.(types.JID)
			if !ok {
				return errors.New("Error casting user jid")
			}
			userJID = payloadId
		case profileUiViewUpdate:
			payloadUpdate, ok := msg.Payload.(profileUiViewUpdate)
			if !ok {
				return errors.New("Error casting profile update")
			}
			pv.queueUpdate(&payloadUpdate)
			return nil

		default:
			return fmt.Errorf("Unexpected profile message payload: %s", msg.Payload)
		}
	}

	ctx, payloadCancel := context.WithTimeout(context.Background(), 5*time.Second)
	go pv.getProfilePayload(ctx, payloadCancel, userJID)

	return nil
}

func (pv *ProfileUiView) queueUpdate(update *profileUiViewUpdate) {
	pv.updateChan <- update
}

func (pv *ProfileUiView) consumeProfileUpdates() {
	for update := range pv.updateChan {
		glib.IdleAdd(func() {
			fmt.Println("consumeProfileUpdates: ", update)
			pv.updateFromProfilePayload(update)
		})
	}
}

func (pv *ProfileUiView) Done() <-chan struct{} {
	return pv.ctx.Done()
}

func (pv *ProfileUiView) Close() {
	if pv.cancel != nil {
		pv.cancel()
	}
}

func (pv *ProfileUiView) handleError(err error) {
	pv.parent.QueueMessage(ErrorView, err)
}

func (pv *ProfileUiView) updateFromProfilePayload(update *profileUiViewUpdate) {
	pv.name.SetLabel(update.name)
	pv.status.SetLabel(update.status)

	hasPicture := update.profileImageBytes != nil
	if hasPicture {
		glibPictureBytes := glib.NewBytes(*update.profileImageBytes)
		pictureTexture, err := gdk.NewTextureFromBytes(glibPictureBytes)
		if err != nil {
			pv.handleError(err)
			return
		}
		pv.profile.SetFromPaintable(pictureTexture)
		pv.profile.SetSizeRequest(pictureTexture.Width(), pictureTexture.Height())
		pv.profile.QueueResize()
	} else {
		pv.profile.Clear()
	}
	pv.noProfile.SetVisible(!hasPicture)
	pv.profile.SetVisible(hasPicture)
}

func (pv *ProfileUiView) getProfilePayload(ctx context.Context, cancel context.CancelFunc, userJID types.JID) {
	client := pv.parent.GetChatClient()
	fmt.Println("ProfileUiView.Update: GetUserInfo: ", userJID)
	users, err := client.GetUserInfo([]types.JID{userJID})
	if err != nil {
		pv.handleError(err)
		return
	}

	userInfo, ok := users[userJID]
	if !ok {
		pv.handleError(fmt.Errorf("Unable to find user profile for: %s\n", userJID))
		return
	}

	fmt.Println("ProfileUiView.Update: GetContact: ", userJID)
	contacts, err := pv.parent.GetContacts()
	if err != nil {
		pv.handleError(err)
		return
	}
	contact, ok := contacts[userJID]
	if !ok {
		contact, err = client.Store.Contacts.GetContact(userJID)
		if err != nil {
			pv.handleError(err)
			return
		}
	}
	fmt.Println("ProfileUiView.Update: Contact: ", contact.FullName)

	statusText := userInfo.Status
	if len(userInfo.Status) == 0 {
		onWhatsAppQuery, err := client.IsOnWhatsApp([]string{userJID.User})
		if err != nil {
			pv.handleError(err)
			return
		}
		if len(onWhatsAppQuery) > 0 && onWhatsAppQuery[0].IsIn {
			statusText = "On WhatsApp"
		} else {
			statusText = "Not on WhatsApp"
		}
	}

	pictureBytes, err := pv.getProfileImageBytes(ctx, userJID)
	if err != nil {
		pv.handleError(err)
		return
	}

	profileUpdate := profileUiViewUpdate{
		name:              contact.FullName,
		status:            statusText,
		profileImageBytes: pictureBytes,
	}
	pv.parent.QueueMessage(ProfileView, profileUpdate)
}

func (pv *ProfileUiView) getProfileImageBytes(ctx context.Context, id types.JID) (*[]byte, error) {
	fmt.Println("ProfileUiView.getProfileImageBytes: id: ", id)
	client := pv.parent.GetChatClient()

	pictureParams := whatsmeow.GetProfilePictureParams{
		Preview:     true,
		IsCommunity: false,
	}
	fmt.Println("ProfileUiView.getProfileImageBytes: GetProfilePictureImage: ", pictureParams)
	pictureInfo, err := client.GetProfilePictureInfo(id, &pictureParams)
	if err != nil {
		if errors.Is(err, whatsmeow.ErrProfilePictureNotSet) {
			fmt.Println("ProfileUiView.getProfileImageBytes: User has no profile image")
			return nil, nil
		}
		return nil, err
	}

	fmt.Println("ProfileUiView.getProfileImageBytes: Get profile image: ", pictureInfo.URL)
	pictureReq, err := http.NewRequestWithContext(ctx, http.MethodGet, pictureInfo.URL, nil)
	if err != nil {
		return nil, err
	}
	pictureResp, err := http.DefaultClient.Do(pictureReq)
	if err != nil {
		return nil, err
	}
	defer pictureResp.Body.Close()

	pictureBytes, err := io.ReadAll(pictureResp.Body)
	if err != nil {
		return nil, err
	}

	return &pictureBytes, nil
}
