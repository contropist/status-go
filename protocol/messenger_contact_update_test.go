package protocol

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/status-im/status-go/deprecation"
	"github.com/status-im/status-go/eth-node/crypto"
	"github.com/status-im/status-go/eth-node/types"
	multiaccountscommon "github.com/status-im/status-go/multiaccounts/common"
	"github.com/status-im/status-go/multiaccounts/settings"
	"github.com/status-im/status-go/protocol/requests"
)

func TestMessengerContactUpdateSuite(t *testing.T) {
	suite.Run(t, new(MessengerContactUpdateSuite))
}

type MessengerContactUpdateSuite struct {
	MessengerBaseTestSuite
}

func (s *MessengerContactUpdateSuite) TestReceiveContactUpdate() {
	theirName := "ens-name.stateofus.eth"

	contactID := types.EncodeHex(crypto.FromECDSAPub(&s.m.identity.PublicKey))

	theirMessenger := s.newMessenger()
	defer TearDownMessenger(&s.Suite, theirMessenger)

	// Set ENS name
	err := theirMessenger.settings.SaveSettingField(settings.PreferredName, theirName)
	s.Require().NoError(err)

	theirContactID := types.EncodeHex(crypto.FromECDSAPub(&theirMessenger.identity.PublicKey))

	response, err := theirMessenger.AddContact(context.Background(), &requests.AddContact{ID: contactID})
	s.Require().NoError(err)
	s.Require().NotNil(response)

	s.Require().Len(response.Contacts, 1)
	contact := response.Contacts[0]
	// It should add the contact
	s.Require().True(contact.added())

	if deprecation.ChatProfileDeprecated {
		// It should a one to one chat
		s.Require().Len(response.Chats(), 1)
		s.Require().False(response.Chats()[0].Active)
	} else {
		// It should create a profile chat & a one to one chat
		s.Require().Len(response.Chats(), 2)
		chats := response.Chats()
		if chats[0].ChatType == ChatTypeOneToOne {
			s.Require().False(chats[0].Active)
		} else {
			s.Require().False(chats[1].Active)
		}
	}

	//// It should a one to one chat
	//s.Require().Len(response.Chats(), 1)
	//s.Require().False(response.Chats()[0].Active)

	// Wait for the message to reach its destination
	response, err = WaitOnMessengerResponse(
		s.m,
		func(r *MessengerResponse) bool { return len(r.Contacts) > 0 },
		"contact request not received",
	)
	s.Require().NoError(err)

	receivedContact := response.Contacts[0]
	s.Require().Equal(theirName, receivedContact.EnsName)
	s.Require().False(receivedContact.ENSVerified)
	s.Require().NotEmpty(receivedContact.LastUpdated)
	s.Require().True(receivedContact.hasAddedUs())

	newPicture := "new-picture"
	err = theirMessenger.SendContactUpdates(context.Background(), newEnsName, newPicture, multiaccountscommon.CustomizationColorRed)
	s.Require().NoError(err)

	// Wait for the message to reach its destination
	response, err = WaitOnMessengerResponse(
		s.m,
		func(r *MessengerResponse) bool {
			return len(r.Contacts) > 0 && response.Contacts[0].ID == theirContactID
		},
		"contact request not received",
	)

	s.Require().NoError(err)

	receivedContact = response.Contacts[0]
	s.Require().Equal(theirContactID, receivedContact.ID)
	s.Require().Equal(newEnsName, receivedContact.EnsName)
	s.Require().False(receivedContact.ENSVerified)
	s.Require().Equal(receivedContact.CustomizationColor, multiaccountscommon.CustomizationColorRed)
	s.Require().NotEmpty(receivedContact.LastUpdated)
}

func (s *MessengerContactUpdateSuite) TestAddContact() {
	contactID := types.EncodeHex(crypto.FromECDSAPub(&s.m.identity.PublicKey))

	theirMessenger := s.newMessenger()
	defer TearDownMessenger(&s.Suite, theirMessenger)

	theirMessenger.account.CustomizationColor = multiaccountscommon.CustomizationColorSky
	response, err := theirMessenger.AddContact(context.Background(), &requests.AddContact{ID: contactID, CustomizationColor: string(multiaccountscommon.CustomizationColorRed)})
	s.Require().NoError(err)
	s.Require().NotNil(response)

	s.Require().Len(response.Contacts, 1)
	contact := response.Contacts[0]

	if deprecation.ChatProfileDeprecated {
		// It adds the one to one chat
		s.Require().Len(response.Chats(), 1)
	} else {
		// It adds the profile chat and the one to one chat
		s.Require().Len(response.Chats(), 2)
	}

	// It should add the contact
	s.Require().True(contact.added())
	s.Require().Equal(contact.CustomizationColor, multiaccountscommon.CustomizationColorRed)

	// Wait for the message to reach its destination
	response, err = WaitOnMessengerResponse(
		s.m,
		func(r *MessengerResponse) bool { return len(r.Contacts) > 0 },
		"contact request not received",
	)
	s.Require().NoError(err)

	receivedContact := response.Contacts[0]
	s.Require().NotEmpty(receivedContact.LastUpdated)
	s.Require().Equal(receivedContact.CustomizationColor, multiaccountscommon.CustomizationColorSky)
}

func (s *MessengerContactUpdateSuite) TestAddContactWithENS() {
	contactID := types.EncodeHex(crypto.FromECDSAPub(&s.m.identity.PublicKey))
	ensName := "blah.stateofus.eth"

	theirMessenger := s.newMessenger()
	defer TearDownMessenger(&s.Suite, theirMessenger)

	s.Require().NoError(theirMessenger.ENSVerified(contactID, ensName))

	response, err := theirMessenger.AddContact(context.Background(), &requests.AddContact{ID: contactID})
	s.Require().NoError(err)
	s.Require().NotNil(response)
	s.Require().Len(response.Contacts, 1)
	s.Require().Equal(ensName, response.Contacts[0].EnsName)
	s.Require().True(response.Contacts[0].ENSVerified)

	s.Require().Len(response.Contacts, 1)
	contact := response.Contacts[0]

	if deprecation.ChatProfileDeprecated {
		// It adds the one to one chat
		s.Require().Len(response.Chats(), 1)
	} else {
		// It adds the profile chat and the one to one chat
		s.Require().Len(response.Chats(), 2)
	}

	// It should add the contact
	s.Require().True(contact.added())

	// Wait for the message to reach its destination
	response, err = WaitOnMessengerResponse(
		s.m,
		func(r *MessengerResponse) bool { return len(r.Contacts) > 0 },
		"contact request not received",
	)
	s.Require().NoError(err)

	receivedContact := response.Contacts[0]
	s.Require().NotEmpty(receivedContact.LastUpdated)
}
