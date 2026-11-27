// Package telegram implements MTProto client management (gotd/td), link parsing,
// chat scouting & quality validation, tgerr-aware joining, captcha auto-solving,
// and the persistent DB-backed scanning pipeline.
package telegram

import (
	"context"
	"time"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
)

// LinkKind classifies normalized Telegram links.
type LinkKind string

const (
	LinkKindUsername LinkKind = "username"
	LinkKindInvite   LinkKind = "invite"
	LinkKindAddList  LinkKind = "addlist"
	LinkKindMessage  LinkKind = "message"
)

// ParsedLink holds canonical fields extracted from any t.me / telegram.me / @username link.
type ParsedLink struct {
	Raw         string   `json:"raw"`
	Normalized  string   `json:"normalized"`
	Kind        LinkKind `json:"kind"`
	Username    string   `json:"username,omitempty"`
	InviteHash  string   `json:"invite_hash,omitempty"`
	AddListSlug string   `json:"addlist_slug,omitempty"`
	ChannelID   int64    `json:"channel_id,omitempty"` // For t.me/c/<id>/<msg_id>
	MessageID   int      `json:"message_id,omitempty"`
}

// TGErrorClass categorizes Telegram RPC / tgerr outcomes cleanly.
type TGErrorClass string

const (
	TGErrorNone               TGErrorClass = ""
	TGErrorAlreadyParticipant TGErrorClass = "already_participant"
	TGErrorInviteRequestSent  TGErrorClass = "invite_request_sent"
	TGErrorBadTarget          TGErrorClass = "bad_target"
	TGErrorChannelsTooMuch    TGErrorClass = "channels_too_much"
	TGErrorBanned             TGErrorClass = "banned"
	TGErrorFloodWait          TGErrorClass = "flood_wait"
	TGErrorTransient          TGErrorClass = "transient"
)

// AuthStep tracks interactive QR / Phone / Code / 2FA setup progress.
type AuthStep string

const (
	AuthStepIdle       AuthStep = "idle"
	AuthStepQRPending  AuthStep = "qr_pending"
	AuthStepPhoneCode  AuthStep = "phone_code"
	AuthStep2FA        AuthStep = "password_2fa"
	AuthStepAuthorized AuthStep = "authorized"
	AuthStepFailed     AuthStep = "failed"
	AuthStepCancelled  AuthStep = "cancelled"
)

// AuthSessionState is the safe frontend/API view of an active MTProto login flow.
type AuthSessionState struct {
	SessionKey  string    `json:"session_key"`
	AccountID   int64     `json:"account_id,omitempty"`
	Step        AuthStep  `json:"step"`
	QRURL       string    `json:"qr_url,omitempty"`
	PhoneMasked string    `json:"phone_masked,omitempty"`
	TGUserID    int64     `json:"tg_user_id,omitempty"`
	Username    string    `json:"username,omitempty"`
	FirstName   string    `json:"first_name,omitempty"`
	Error       string    `json:"error,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// DialogPreview describes a chat or folder entry returned by "Add My Chats".
type DialogPreview struct {
	TGChatID    int64           `json:"tg_chat_id"`
	AccessHash  int64           `json:"access_hash,omitempty"`
	Username    string          `json:"username,omitempty"`
	Title       string          `json:"title"`
	ChatType    domain.ChatType `json:"chat_type"`
	MemberCount int             `json:"member_count"`
	HasComments bool            `json:"has_comments"`
	LinkedID    int64           `json:"linked_chat_id,omitempty"`
	FolderNames []string        `json:"folder_names,omitempty"`
}

// ChatQualityReport summarizes pre-join or post-resolve chat health metrics.
type ChatQualityReport struct {
	MemberCount        int     `json:"member_count"`
	MsgVelocityPerHour float64 `json:"msg_velocity_per_hour"`
	HasComments        bool    `json:"has_comments"`
	LinkedChatID       int64   `json:"linked_chat_id,omitempty"`
	Passed             bool    `json:"passed"`
	Reason             string  `json:"reason,omitempty"`
}

// CaptchaButton represents an inline or reply button on a Telegram message.
type CaptchaButton struct {
	Text         string `json:"text"`
	CallbackData []byte `json:"callback_data,omitempty"`
	Row          int    `json:"row"`
	Col          int    `json:"col"`
}

// IncomingMessage is the normalized message envelope fed to Captcha & Tier-0/LLM pipeline.
type IncomingMessage struct {
	AccountID       int64           `json:"account_id"`
	ChatID          int64           `json:"chat_id"`
	TGChatID        int64           `json:"tg_chat_id"`
	AccessHash      int64           `json:"access_hash,omitempty"`
	ChatTitle       string          `json:"chat_title"`
	ChatUsername    string          `json:"chat_username,omitempty"`
	ChatType        domain.ChatType `json:"chat_type"`
	MsgID           int             `json:"msg_id"`
	AuthorID        int64           `json:"author_id"`
	AuthorUsername  string          `json:"author_username,omitempty"`
	AuthorName      string          `json:"author_name"`
	IsBot           bool            `json:"is_bot"`
	IsService       bool            `json:"is_service"`
	Text            string          `json:"text"`
	Timestamp       time.Time       `json:"timestamp"`
	Buttons         []CaptchaButton `json:"buttons,omitempty"`
	HasPhoto        bool            `json:"has_photo,omitempty"`
	CaptchaImageURL string          `json:"captcha_image_url,omitempty"`
}

// AccountTransport abstracts MTProto RPC calls per account so gotd/td and mock tests
// share the exact same scout, joiner, captcha, and pipeline logic.
type AccountTransport interface {
	AccountID() int64
	ListDialogs(ctx context.Context, limit int) ([]DialogPreview, error)
	SearchGlobal(ctx context.Context, query string, limit int) ([]DialogPreview, error)
	ResolveUsername(ctx context.Context, username string) (DialogPreview, error)
	CheckChatInvite(ctx context.Context, hash string) (DialogPreview, bool, error)
	JoinChannel(ctx context.Context, username string, tgChatID, accessHash int64) (DialogPreview, error)
	ImportInvite(ctx context.Context, hash string) (DialogPreview, error)
	ImportChatList(ctx context.Context, slug string) ([]DialogPreview, error)
	GetRecentMessages(ctx context.Context, tgChatID, accessHash int64, minID, limit int) ([]IncomingMessage, error)
	ReadHistory(ctx context.Context, tgChatID, accessHash int64, maxID int) error
	PressButton(ctx context.Context, tgChatID, accessHash int64, msgID int, callbackData []byte) error
	SendReply(ctx context.Context, tgChatID, accessHash int64, replyToMsgID int, text string) error
	DownloadMessageImage(ctx context.Context, tgChatID, accessHash int64, msgID int) (string, error)
}

// JoinOutcome captures the classified result of joining a link.
type JoinOutcome struct {
	Status       domain.ChatStatus `json:"status"`
	Chats        []domain.Chat     `json:"chats,omitempty"`
	ErrorClass   TGErrorClass      `json:"error_class,omitempty"`
	FloodWait    time.Duration     `json:"flood_wait,omitempty"`
	ErrorMessage string            `json:"error_message,omitempty"`
}

// PipelineStats reports live MTProto worker state for API/Bot status views.
type PipelineStats struct {
	Running        bool   `json:"running"`
	Paused         bool   `json:"paused"`
	ActiveAccounts int    `json:"active_accounts"`
	FloodedAccount int    `json:"flooded_accounts"`
	ProcessedMsgs  int64  `json:"processed_msgs"`
	QualifiedLeads int64  `json:"qualified_leads"`
	LastError      string `json:"last_error,omitempty"`
}
