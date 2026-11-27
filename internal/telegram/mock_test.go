package telegram

import (
	"context"
	"fmt"
)

type mockTransport struct {
	id            int64
	dialogs       func(context.Context, int) ([]DialogPreview, error)
	search        func(context.Context, string, int) ([]DialogPreview, error)
	resolve       func(context.Context, string) (DialogPreview, error)
	checkInvite   func(context.Context, string) (DialogPreview, bool, error)
	joinChannel   func(context.Context, string, int64, int64) (DialogPreview, error)
	importInvite  func(context.Context, string) (DialogPreview, error)
	importList    func(context.Context, string) ([]DialogPreview, error)
	recent        func(context.Context, int64, int64, int, int) ([]IncomingMessage, error)
	readHistory   func(context.Context, int64, int64, int) error
	pressButton   func(context.Context, int64, int64, int, []byte) error
	sendReply     func(context.Context, int64, int64, int, string) error
	downloadImage func(context.Context, int64, int64, int) (string, error)
}

func (m *mockTransport) AccountID() int64 {
	if m.id == 0 {
		return 1
	}
	return m.id
}

func (m *mockTransport) ListDialogs(ctx context.Context, limit int) ([]DialogPreview, error) {
	if m.dialogs != nil {
		return m.dialogs(ctx, limit)
	}
	return nil, nil
}

func (m *mockTransport) SearchGlobal(ctx context.Context, query string, limit int) ([]DialogPreview, error) {
	if m.search != nil {
		return m.search(ctx, query, limit)
	}
	return nil, nil
}

func (m *mockTransport) ResolveUsername(ctx context.Context, username string) (DialogPreview, error) {
	if m.resolve != nil {
		return m.resolve(ctx, username)
	}
	return DialogPreview{}, fmt.Errorf("mock: resolve %q not configured", username)
}

func (m *mockTransport) CheckChatInvite(ctx context.Context, hash string) (DialogPreview, bool, error) {
	if m.checkInvite != nil {
		return m.checkInvite(ctx, hash)
	}
	return DialogPreview{}, false, fmt.Errorf("mock: check invite not configured")
}

func (m *mockTransport) JoinChannel(ctx context.Context, username string, tgChatID, accessHash int64) (DialogPreview, error) {
	if m.joinChannel != nil {
		return m.joinChannel(ctx, username, tgChatID, accessHash)
	}
	return DialogPreview{TGChatID: tgChatID, AccessHash: accessHash, Username: username, Title: username}, nil
}

func (m *mockTransport) ImportInvite(ctx context.Context, hash string) (DialogPreview, error) {
	if m.importInvite != nil {
		return m.importInvite(ctx, hash)
	}
	return DialogPreview{}, fmt.Errorf("mock: import invite not configured")
}

func (m *mockTransport) ImportChatList(ctx context.Context, slug string) ([]DialogPreview, error) {
	if m.importList != nil {
		return m.importList(ctx, slug)
	}
	return nil, fmt.Errorf("mock: import list not configured")
}

func (m *mockTransport) GetRecentMessages(ctx context.Context, tgChatID, accessHash int64, minID, limit int) ([]IncomingMessage, error) {
	if m.recent != nil {
		return m.recent(ctx, tgChatID, accessHash, minID, limit)
	}
	return nil, nil
}

func (m *mockTransport) ReadHistory(ctx context.Context, tgChatID, accessHash int64, maxID int) error {
	if m.readHistory != nil {
		return m.readHistory(ctx, tgChatID, accessHash, maxID)
	}
	return nil
}

func (m *mockTransport) PressButton(ctx context.Context, tgChatID, accessHash int64, msgID int, callbackData []byte) error {
	if m.pressButton != nil {
		return m.pressButton(ctx, tgChatID, accessHash, msgID, callbackData)
	}
	return nil
}

func (m *mockTransport) SendReply(ctx context.Context, tgChatID, accessHash int64, replyToMsgID int, text string) error {
	if m.sendReply != nil {
		return m.sendReply(ctx, tgChatID, accessHash, replyToMsgID, text)
	}
	return nil
}

func (m *mockTransport) DownloadMessageImage(ctx context.Context, tgChatID, accessHash int64, msgID int) (string, error) {
	if m.downloadImage != nil {
		return m.downloadImage(ctx, tgChatID, accessHash, msgID)
	}
	return "", fmt.Errorf("mock: download image not configured")
}
