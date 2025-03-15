package server

import (
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strconv"

	"github.com/status-im/status-go/ipfs"
	"github.com/status-im/status-go/logutils"
	"github.com/status-im/status-go/multiaccounts"
	"github.com/status-im/status-go/protocol/common"
	"github.com/status-im/status-go/protocol/protobuf"
	"github.com/status-im/status-go/services/wallet/thirdparty"
	"github.com/status-im/status-go/signal"
)

type MediaServerOption func(*MediaServer)

func WithMediaServerDisableTLS(disableTLS bool) MediaServerOption {
	return func(s *MediaServer) {
		s.disableTLS = disableTLS
	}
}

type MediaServer struct {
	Server

	db                          *sql.DB
	downloader                  *ipfs.Downloader
	multiaccountsDB             *multiaccounts.Database
	walletDB                    *sql.DB
	communityImagesReader       func(communityID string) (map[string]*protobuf.IdentityImage, error)
	communityTokenReader        func(communityID string) ([]*protobuf.CommunityTokenMetadata, error)
	communityImageVersionReader func(communityID string) uint32

	// disableTLS controls whether the media server uses HTTP instead of HTTPS.
	// Set to true to avoid TLS certificate issues with react-native-fast-image
	// on Android, which has limitations with dynamic certificate updates.
	// Pls check doc/use-status-backend-server.md in status-mobile for more details
	disableTLS bool
}

func initMediaCertificate(disableTLS bool) (*tls.Certificate, error) {
	if disableTLS {
		return nil, nil
	}

	cert, _, err := generateMediaTLSCert()
	if err != nil {
		return nil, err
	}
	return cert, nil
}

// NewMediaServer returns a *MediaServer
func NewMediaServer(db *sql.DB, downloader *ipfs.Downloader, multiaccountsDB *multiaccounts.Database, walletDB *sql.DB, opts ...MediaServerOption) (*MediaServer, error) {

	s := &MediaServer{
		disableTLS:      false,
		db:              db,
		downloader:      downloader,
		multiaccountsDB: multiaccountsDB,
		walletDB:        walletDB,
	}

	for _, opt := range opts {
		opt(s)
	}

	cert, err := initMediaCertificate(s.disableTLS)
	if err != nil {
		return nil, err
	}
	s.Server = NewServer(
		cert,
		Localhost,
		signal.SendMediaServerStarted,
		logutils.ZapLogger().Named("MediaServer"),
	)

	s.SetHandlers(HandlerPatternMap{
		accountImagesPath:                   handleAccountImages(s.multiaccountsDB, s.logger),
		accountInitialsPath:                 handleAccountInitials(s.multiaccountsDB, s.logger),
		audioPath:                           handleAudio(s.db, s.logger),
		contactImagesPath:                   handleContactImages(s.db, s.logger),
		discordAttachmentsPath:              handleDiscordAttachment(s.db, s.logger),
		discordAuthorsPath:                  handleDiscordAuthorAvatar(s.db, s.logger),
		generateQRCode:                      handleQRCodeGeneration(s.multiaccountsDB, s.logger),
		imagesPath:                          handleImage(s.db, s.logger),
		ipfsPath:                            handleIPFS(s.downloader, s.logger),
		LinkPreviewThumbnailPath:            handleLinkPreviewThumbnail(s.db, s.logger),
		LinkPreviewFaviconPath:              handleLinkPreviewFavicon(s.db, s.logger),
		StatusLinkPreviewThumbnailPath:      handleStatusLinkPreviewThumbnail(s.db, s.logger),
		communityTokenImagesPath:            handleCommunityTokenImages(s.db, s.logger),
		communityDescriptionImagesPath:      handleCommunityDescriptionImagesPath(s.db, s.getCommunityImage, s.logger),
		communityDescriptionTokenImagesPath: handleCommunityDescriptionTokenImagesPath(s.db, s.getCommunityTokens, s.logger),
		walletCommunityImagesPath:           handleWalletCommunityImages(s.walletDB, s.logger),
		walletCollectionImagesPath:          handleWalletCollectionImages(s.walletDB, s.logger),
		walletCollectibleImagesPath:         handleWalletCollectibleImages(s.walletDB, s.logger),
	})

	return s, nil
}

func (s *MediaServer) MakeBaseURL() *url.URL {
	return &url.URL{
		Scheme: map[bool]string{true: "http", false: "https"}[s.disableTLS],
		Host:   s.mustGetHost(),
	}
}

func (s *MediaServer) SetCommunityImageVersionReader(getFunc func(communityID string) uint32) {
	s.communityImageVersionReader = getFunc
}

func (s *MediaServer) getCommunityImageVersion(communityID string) uint32 {
	if s.communityImageVersionReader == nil {
		return 0
	}
	return s.communityImageVersionReader(communityID)
}

func (s *MediaServer) SetCommunityImageReader(getFunc func(communityID string) (map[string]*protobuf.IdentityImage, error)) {
	s.communityImagesReader = getFunc
}

func (s *MediaServer) getCommunityImage(communityID string) (map[string]*protobuf.IdentityImage, error) {
	if s.communityImagesReader == nil {
		return nil, errors.New("community image reader not set")
	}
	return s.communityImagesReader(communityID)
}

func (s *MediaServer) SetCommunityTokensReader(getFunc func(communityID string) ([]*protobuf.CommunityTokenMetadata, error)) {
	s.communityTokenReader = getFunc
}

func (s *MediaServer) getCommunityTokens(communityID string) ([]*protobuf.CommunityTokenMetadata, error) {
	if s.communityTokenReader == nil {
		return nil, errors.New("community token reader not set")
	}
	return s.communityTokenReader(communityID)
}

func (s *MediaServer) MakeImageServerURL() string {
	u := s.MakeBaseURL()
	u.Path = basePath + "/"
	return u.String()
}

func (s *MediaServer) MakeImageURL(id string) string {
	u := s.MakeBaseURL()
	u.Path = imagesPath
	u.RawQuery = url.Values{"messageId": {id}}.Encode()

	return u.String()
}

func (s *MediaServer) MakeLinkPreviewThumbnailURL(msgID string, previewURL string) string {
	u := s.MakeBaseURL()
	u.Path = LinkPreviewThumbnailPath
	u.RawQuery = url.Values{"message-id": {msgID}, "url": {previewURL}}.Encode()
	return u.String()
}

func (s *MediaServer) MakeStatusLinkPreviewThumbnailURL(msgID string, previewURL string, imageID common.MediaServerImageID) string {
	u := s.MakeBaseURL()
	u.Path = StatusLinkPreviewThumbnailPath
	u.RawQuery = url.Values{"message-id": {msgID}, "url": {previewURL}, "image-id": {string(imageID)}}.Encode()
	return u.String()
}

func (s *MediaServer) MakeLinkPreviewFaviconURL(msgID string, previewURL string) string {
	u := s.MakeBaseURL()
	u.Path = LinkPreviewFaviconPath
	u.RawQuery = url.Values{"message-id": {msgID}, "url": {previewURL}}.Encode()
	return u.String()
}

func (s *MediaServer) MakeDiscordAuthorAvatarURL(authorID string) string {
	u := s.MakeBaseURL()
	u.Path = discordAuthorsPath
	u.RawQuery = url.Values{"authorId": {authorID}}.Encode()

	return u.String()
}

func (s *MediaServer) MakeDiscordAttachmentURL(messageID string, id string) string {
	u := s.MakeBaseURL()
	u.Path = discordAttachmentsPath
	u.RawQuery = url.Values{"messageId": {messageID}, "attachmentId": {id}}.Encode()

	return u.String()
}

func (s *MediaServer) MakeAudioURL(id string) string {
	u := s.MakeBaseURL()
	u.Path = audioPath
	u.RawQuery = url.Values{"messageId": {id}}.Encode()

	return u.String()
}

func (s *MediaServer) MakeStickerURL(stickerHash string) string {
	u := s.MakeBaseURL()
	u.Path = ipfsPath
	u.RawQuery = url.Values{"hash": {stickerHash}}.Encode()

	return u.String()
}

func (s *MediaServer) MakeQRURL(qurul string,
	allowProfileImage string,
	level string,
	size string,
	keyUID string,
	imageName string) string {
	u := s.MakeBaseURL()
	u.Path = generateQRCode
	u.RawQuery = url.Values{"url": {qurul},
		"level":             {level},
		"allowProfileImage": {allowProfileImage},
		"size":              {size},
		"keyUid":            {keyUID},
		"imageName":         {imageName}}.Encode()

	return u.String()
}

func (s *MediaServer) MakeContactImageURL(publicKey string, imageType string, imageClock uint64) string {
	u := s.MakeBaseURL()
	u.Path = contactImagesPath
	u.RawQuery = url.Values{"publicKey": {publicKey}, "imageName": {imageType}, "clock": {fmt.Sprint(imageClock)}}.Encode()

	return u.String()
}

func (s *MediaServer) MakeCommunityTokenImagesURL(communityID string, chainID uint64, symbol string) string {
	u := s.MakeBaseURL()
	u.Path = communityTokenImagesPath
	u.RawQuery = url.Values{
		"communityID": {communityID},
		"chainID":     {strconv.FormatUint(chainID, 10)},
		"symbol":      {symbol},
	}.Encode()

	return u.String()
}

func (s *MediaServer) MakeCommunityImageURL(communityID, name string) string {
	u := s.MakeBaseURL()
	u.Path = communityDescriptionImagesPath
	u.RawQuery = url.Values{
		"communityID": {communityID},
		"name":        {name},
		"version":     {fmt.Sprintf("%d", (s.getCommunityImageVersion(communityID)))},
	}.Encode()

	return u.String()
}

func (s *MediaServer) MakeCommunityDescriptionTokenImageURL(communityID, symbol string) string {
	u := s.MakeBaseURL()
	u.Path = communityDescriptionTokenImagesPath
	u.RawQuery = url.Values{
		"communityID": {communityID},
		"symbol":      {symbol},
	}.Encode()

	return u.String()
}

func (s *MediaServer) MakeWalletCommunityImagesURL(communityID string) string {
	u := s.MakeBaseURL()
	u.Path = walletCommunityImagesPath
	u.RawQuery = url.Values{
		"communityID": {communityID},
	}.Encode()

	return u.String()
}

func (s *MediaServer) MakeWalletCollectionImagesURL(contractID thirdparty.ContractID) string {
	u := s.MakeBaseURL()
	u.Path = walletCollectionImagesPath
	u.RawQuery = url.Values{
		"chainID":         {contractID.ChainID.String()},
		"contractAddress": {contractID.Address.Hex()},
	}.Encode()

	return u.String()
}

func (s *MediaServer) MakeWalletCollectibleImagesURL(collectibleID thirdparty.CollectibleUniqueID) string {
	u := s.MakeBaseURL()
	u.Path = walletCollectibleImagesPath
	u.RawQuery = url.Values{
		"chainID":         {collectibleID.ContractID.ChainID.String()},
		"contractAddress": {collectibleID.ContractID.Address.Hex()},
		"tokenID":         {collectibleID.TokenID.String()},
	}.Encode()

	return u.String()
}
