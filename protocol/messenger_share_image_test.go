package protocol

import (
	"context"
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/status-im/status-go/eth-node/types"
	"github.com/status-im/status-go/protocol/common"
	"github.com/status-im/status-go/protocol/protobuf"
	"github.com/status-im/status-go/protocol/requests"
	// "github.com/status-im/status-go/protocol/requests"
)

func TestMessengerShareMessageSuite(t *testing.T) {
	suite.Run(t, new(MessengerShareMessageSuite))
}

type MessengerShareMessageSuite struct {
	MessengerBaseTestSuite
}

func buildImageMessage(s *MessengerShareMessageSuite, chat Chat) *common.Message {
	file, err := os.Open("../_assets/tests/test.jpg")
	s.Require().NoError(err)
	defer file.Close()

	payload, err := ioutil.ReadAll(file)
	s.Require().NoError(err)

	clock, timestamp := chat.NextClockAndTimestamp(&testTimeSource{})
	message := common.NewMessage()
	message.ChatId = chat.ID
	message.Clock = clock
	message.Timestamp = timestamp
	message.WhisperTimestamp = clock
	message.LocalChatID = chat.ID
	message.MessageType = protobuf.MessageType_ONE_TO_ONE
	message.ContentType = protobuf.ChatMessage_IMAGE
	message.Text = "An image"

	image := protobuf.ImageMessage{
		Payload: payload,
		Format:  protobuf.ImageFormat_JPEG,
		AlbumId: "some-album-id",
		Width:   1200,
		Height:  1000,
	}
	message.Payload = &protobuf.ChatMessage_Image{Image: &image}
	return message
}

func (s *MessengerShareMessageSuite) TestImageMessageSharing() {
	theirMessenger := s.newMessenger()
	defer TearDownMessenger(&s.Suite, theirMessenger)

	theirChat := CreateOneToOneChat("Their 1TO1", &s.privateKey.PublicKey, s.m.transport)
	err := theirMessenger.SaveChat(theirChat)
	s.Require().NoError(err)

	ourChat := CreateOneToOneChat("Our 1TO1", &theirMessenger.identity.PublicKey, s.m.transport)
	err = s.m.SaveChat(ourChat)
	s.Require().NoError(err)

	inputMessage := buildImageMessage(s, *ourChat)
	err = s.m.SaveChat(ourChat)
	s.NoError(err)
	response, err := s.m.SendChatMessage(context.Background(), inputMessage)
	s.NoError(err)
	s.Require().Equal(1, len(response.Messages()), "it returns the message")

	outputMessage := response.Messages()[0]

	MessageID := outputMessage.ID

	s.Require().NoError(err)
	s.Require().Len(response.Messages(), 1)

	response, err = WaitOnMessengerResponse(
		theirMessenger,
		func(r *MessengerResponse) bool { return len(r.messages) > 0 },
		"no messages",
	)

	s.Require().NoError(err)
	s.Require().Len(response.Chats(), 1)
	s.Require().Len(response.Messages(), 1)
	s.Require().Equal(response.Messages()[0].Text, "An image")

	shareResponse, err := s.m.ShareImageMessage(
		&requests.ShareImageMessage{
			MessageID: MessageID,
			Users:     []types.HexBytes{common.PubkeyToHexBytes(&theirMessenger.identity.PublicKey)},
		},
	)

	s.NoError(err)
	s.Require().NotNil(shareResponse)
	s.Require().Len(shareResponse.Messages(), 1)

	response, err = WaitOnMessengerResponse(
		theirMessenger,
		func(r *MessengerResponse) bool { return len(r.messages) > 0 },
		"no messages",
	)

	s.Require().NoError(err)
	s.Require().Len(response.Chats(), 1)
	s.Require().Len(response.Messages(), 1)
	s.Require().Equal(response.Messages()[0].Text, "This message has been shared with you")
}
