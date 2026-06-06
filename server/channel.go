package server

import (
	"context"
	"fmt"

	"go.klarlabs.de/mcp/protocol"
)

// ChannelMessage represents a message pushed to the client through a channel.
type ChannelMessage struct {
	// Channel is the channel identifier (free-form string).
	Channel string `json:"channel"`
	// Content is the message content.
	Content Content `json:"message"`
	// Data contains optional structured data.
	Data map[string]any `json:"data,omitempty"`
	// Priority indicates the message priority ("normal" or "high").
	Priority string `json:"priority,omitempty"`
}

// ChannelSender allows tool handlers to push messages to clients proactively.
type ChannelSender struct {
	session *Session
}

// NewChannelSender creates a new ChannelSender for the given session.
func NewChannelSender(session *Session) *ChannelSender {
	return &ChannelSender{session: session}
}

// Send pushes a channel message to the client.
// Returns an error if the client doesn't support channels.
func (c *ChannelSender) Send(msg *ChannelMessage) error {
	if c == nil || c.session == nil {
		return fmt.Errorf("channels not available: no session")
	}

	if !c.session.SupportsFeature("channels") {
		return fmt.Errorf("client does not support channels")
	}

	return c.session.notifier.SendNotification(protocol.MethodChannelMessage, msg)
}

// SendText is a convenience method for sending a text message on a channel.
func (c *ChannelSender) SendText(channel, text string) error {
	return c.Send(&ChannelMessage{
		Channel: channel,
		Content: NewTextContent(text),
	})
}

// channelKey is the context key for the channel sender.
type channelKey struct{}

// ContextWithChannel returns a context with the channel sender attached.
func ContextWithChannel(ctx context.Context, cs *ChannelSender) context.Context {
	return context.WithValue(ctx, channelKey{}, cs)
}

// ChannelFromContext returns the channel sender from context, or nil if not available.
func ChannelFromContext(ctx context.Context) *ChannelSender {
	cs, _ := ctx.Value(channelKey{}).(*ChannelSender)
	return cs
}
