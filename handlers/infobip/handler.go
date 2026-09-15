package infobip

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/buger/jsonparser"
	"github.com/nyaruka/courier"
	"github.com/nyaruka/courier/handlers"
	"github.com/nyaruka/gocommon/httpx"
	"github.com/nyaruka/gocommon/urns"
)

const (
	configTransliteration = "transliteration"
	// Infobip CPaaS-X application + entity tagging. Both are optional per-channel config values that get echoed
	// back on delivery reports and drive Infobip's per-application routing / per-entity reporting. When either
	// is set on the channel it's attached under messages[].options.platform for both SMS and MMS sends.
	configApplicationID = "application_id"
	configEntityID      = "entity_id"
)

// Infobip webhook signing: when the channel has a shared secret configured (courier.ConfigSecret, "secret" in
// channel config JSON) Infobip is expected to sign each inbound webhook body with HMAC-SHA256 and send the digest
// in the X-Hub-Signature header as "SHA256=<hex>". The prefix and hex are conventionally uppercase but we compare
// case-insensitively. If the secret is empty we skip the check (opt-in enforcement so migration channels can be
// enabled independently).
const (
	signatureHeader   = "X-Hub-Signature"
	signaturePrefix   = "SHA256="
	maxWebhookBodyLen = 1024 * 1024
)

func init() {
	courier.RegisterHandler(newHandler())
}

type handler struct {
	handlers.BaseHandler
}

func newHandler() courier.ChannelHandler {
	return &handler{handlers.NewBaseHandler(courier.ChannelType("IB"), "Infobip")}
}

// Initialize is called by the engine once everything is loaded
func (h *handler) Initialize(s courier.Server) error {
	h.SetServer(s)
	// One endpoint for every Messages API webhook event. Both inbound (MO) messages and delivery reports arrive
	// in the same `{"results": [...]}` envelope with a per-result `event` discriminator, so a single dispatcher
	// classifies each result and hands it to the right backend write. Same pattern the meta handler uses.
	s.AddHandlerRoute(h, http.MethodPost, "receive", courier.ChannelLogTypeMultiReceive, h.withSignatureCheck(handlers.JSONPayload(h, h.receiveEvents)))
	return nil
}

// withSignatureCheck wraps a handler so the Infobip X-Hub-Signature (HMAC-SHA256 of the raw body) is verified
// before the inner handler runs. Skips silently if the channel has no shared secret configured. On mismatch or
// missing header (when a secret IS configured) the request is rejected 401 without touching the inner handler.
func (h *handler) withSignatureCheck(inner courier.ChannelHandleFunc) courier.ChannelHandleFunc {
	return func(ctx context.Context, c courier.Channel, w http.ResponseWriter, r *http.Request, clog *courier.ChannelLog) ([]courier.Event, error) {
		if err := h.validateSignature(c, r); err != nil {
			return nil, courier.WriteAndLogUnauthorized(w, r, c, err)
		}
		return inner(ctx, c, w, r, clog)
	}
}

// validateSignature verifies X-Hub-Signature against HMAC-SHA256(body, channel_secret). The body is read via
// handlers.ReadBody which restores r.Body for the downstream JSON parser. A missing secret on the channel is
// the "not opted in yet" case and returns nil - opt-in per channel keeps this safe to merge before every
// channel is fully cut over.
func (h *handler) validateSignature(c courier.Channel, r *http.Request) error {
	secret := c.StringConfigForKey(courier.ConfigSecret, "")
	if secret == "" {
		return nil
	}
	header := strings.TrimSpace(r.Header.Get(signatureHeader))
	if header == "" {
		return fmt.Errorf("missing %s header", signatureHeader)
	}
	if !strings.HasPrefix(strings.ToUpper(header), signaturePrefix) {
		return fmt.Errorf("invalid %s format, expected %s<hex>", signatureHeader, signaturePrefix)
	}
	received := header[len(signaturePrefix):]
	body, err := handlers.ReadBody(r, maxWebhookBodyLen)
	if err != nil {
		return fmt.Errorf("unable to read request body: %s", err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(strings.ToLower(expected)), []byte(strings.ToLower(received))) {
		return fmt.Errorf("invalid %s signature", signatureHeader)
	}
	return nil
}

// statusMapping translates Messages API delivery status group names to courier's status enum. Group names
// are the same across the SMS API v3 and Messages API DLR envelopes, so this mapping is unchanged from the
// previous handler - only the surrounding envelope shape changed.
var statusMapping = map[string]courier.MsgStatus{
	"PENDING":       courier.MsgStatusSent,
	"EXPIRED":       courier.MsgStatusSent,
	"DELIVERED":     courier.MsgStatusDelivered,
	"REJECTED":      courier.MsgStatusFailed,
	"UNDELIVERABLE": courier.MsgStatusFailed,
}

// ibEventsPayload is the top-level envelope for every Messages API webhook. The events themselves are a
// discriminated union (MO / DELIVERY / TYPING_STARTED / SEEN / ...), so we defer per-result parsing until
// after we've read the discriminator - json.RawMessage keeps the bytes intact for the re-parse.
type ibEventsPayload struct {
	Results []json.RawMessage `json:"results"`
}

// ibEventEnvelope is the shared prefix on every event that we peek at to route to the right typed struct.
type ibEventEnvelope struct {
	Event   string `json:"event"`   // "MO", "DELIVERY", "TYPING_STARTED", "SEEN", ...
	Channel string `json:"channel"` // "SMS" (and others we don't send today)
}

// ibMOEvent is a Messages API "message-originated" inbound - a message a user sent to our number.
type ibMOEvent struct {
	Event       string        `json:"event"`
	Channel     string        `json:"channel"`
	Sender      string        `json:"sender"`
	Destination string        `json:"destination"`
	MessageID   string        `json:"messageId"`
	ReceivedAt  string        `json:"receivedAt"`
	Content     []ibMOContent `json:"content"`
}

// ibMOContent is a single content segment on an MO event. Messages API inbound is a list of typed segments
// (text and URL for SMS/MMS; more for richer channels). We stitch text segments together and hand URL
// segments to courier as attachments.
type ibMOContent struct {
	Type string `json:"type"` // "TEXT", "URL", ...
	Text string `json:"text,omitempty"`
	URL  string `json:"url,omitempty"`
	// Content-type on URL segments so courier's attachment format (`<mime>:<url>`) has a real mime.
	ContentType string `json:"contentType,omitempty"`
}

// ibDeliveryEvent is a Messages API delivery report.
type ibDeliveryEvent struct {
	Event     string           `json:"event"`
	Channel   string           `json:"channel"`
	MessageID string           `json:"messageId"`
	Status    ibDeliveryStatus `json:"status"`
}

type ibDeliveryStatus struct {
	GroupID   int    `json:"groupId"`
	GroupName string `json:"groupName"`
	Name      string `json:"name"`
}

// receiveEvents dispatches every result in a Messages API webhook to the right pipeline: MO → NewIncomingMsg
// + WriteMsg, DELIVERY → NewStatusUpdateByExternalID + WriteStatusUpdate. Unknown or non-actionable event
// types (TYPING_STARTED, SEEN, etc.) are logged and skipped, not errored - dropping the whole batch because
// one segment is a typing indicator would lose real messages next to it.
func (h *handler) receiveEvents(ctx context.Context, channel courier.Channel, w http.ResponseWriter, r *http.Request, payload *ibEventsPayload, clog *courier.ChannelLog) ([]courier.Event, error) {
	if len(payload.Results) == 0 {
		return nil, handlers.WriteAndLogRequestIgnored(ctx, h, channel, w, r, "ignoring request, no results")
	}

	var msgs []courier.MsgIn
	var events []courier.Event
	data := []any{}

	for _, raw := range payload.Results {
		env := ibEventEnvelope{}
		if err := json.Unmarshal(raw, &env); err != nil {
			// One malformed result shouldn't fail the whole batch - log and skip.
			slog.Warn("infobip: unable to parse event envelope", "error", err, "channel_uuid", channel.UUID())
			continue
		}

		switch env.Event {
		case "MO":
			mo := ibMOEvent{}
			if err := json.Unmarshal(raw, &mo); err != nil {
				slog.Warn("infobip: unable to parse MO event", "error", err, "channel_uuid", channel.UUID())
				continue
			}
			msg, err := h.buildIncomingMsg(channel, &mo, clog)
			if err != nil {
				return nil, handlers.WriteAndLogRequestError(ctx, h, channel, w, r, err)
			}
			if msg == nil {
				continue // event carried no text or attachments
			}
			msgs = append(msgs, msg)

		case "DELIVERY":
			dr := ibDeliveryEvent{}
			if err := json.Unmarshal(raw, &dr); err != nil {
				slog.Warn("infobip: unable to parse DELIVERY event", "error", err, "channel_uuid", channel.UUID())
				continue
			}
			msgStatus, found := statusMapping[dr.Status.GroupName]
			if !found {
				// Unknown status group - log at info and skip so the whole batch still gets processed. A
				// hard error here would break other DLRs sharing the payload.
				slog.Info("infobip: unknown delivery status", "group_name", dr.Status.GroupName, "message_id", dr.MessageID, "channel_uuid", channel.UUID())
				continue
			}
			status := h.Backend().NewStatusUpdateByExternalID(channel, dr.MessageID, msgStatus, clog)
			if err := h.Backend().WriteStatusUpdate(ctx, status); err != nil {
				return nil, err
			}
			events = append(events, status)
			data = append(data, courier.NewStatusData(status))

		default:
			// TYPING_STARTED, TYPING_STOPPED, SEEN, etc. - not actionable for SMS, just log at debug level.
			slog.Debug("infobip: ignored event type", "event", env.Event, "channel", env.Channel, "channel_uuid", channel.UUID())
		}
	}

	// If any inbound msgs came through, handler.WriteMsgsAndResponse writes them AND writes the response,
	// so it's the terminating call. When there are only DLRs (or nothing actionable), fall through to the
	// generic data response so the DLR data still comes back in the body.
	if len(msgs) > 0 {
		return handlers.WriteMsgsAndResponse(ctx, h, msgs, w, r, clog)
	}
	if len(events) == 0 {
		return nil, handlers.WriteAndLogRequestIgnored(ctx, h, channel, w, r, "no actionable events in batch")
	}
	return events, courier.WriteDataResponse(w, http.StatusOK, "events handled", data)
}

// buildIncomingMsg turns a Messages API MO event into a courier MsgIn ready to write. Returns (nil, nil) if
// the event carries no text and no attachments so the caller can skip it without erroring.
func (h *handler) buildIncomingMsg(channel courier.Channel, mo *ibMOEvent, clog *courier.ChannelLog) (courier.MsgIn, error) {
	urn, err := urns.ParsePhone(mo.Sender, channel.Country(), true, false)
	if err != nil {
		return nil, err
	}

	var textParts []string
	var attachments []string
	for _, seg := range mo.Content {
		switch seg.Type {
		case "TEXT":
			if seg.Text != "" {
				textParts = append(textParts, seg.Text)
			}
		case "URL":
			if seg.URL != "" {
				ct := seg.ContentType
				if ct == "" {
					ct = "application/octet-stream"
				}
				attachments = append(attachments, fmt.Sprintf("%s:%s", ct, seg.URL))
			}
		}
	}
	text := strings.Join(textParts, "\n")
	if text == "" && len(attachments) == 0 {
		return nil, nil
	}

	// receivedAt is "yyyy-MM-dd'T'HH:mm:ss.SSSZ" (RFC3339 with millisecond precision) per Messages API docs.
	// Fall back to now if the field is missing or malformed rather than dropping the msg over a timestamp.
	date := time.Now()
	if mo.ReceivedAt != "" {
		if parsed, perr := time.Parse("2006-01-02T15:04:05.000-0700", mo.ReceivedAt); perr == nil {
			date = parsed
		} else if parsed, perr := time.Parse(time.RFC3339Nano, mo.ReceivedAt); perr == nil {
			date = parsed
		}
	}

	msg := h.Backend().NewIncomingMsg(channel, urn, text, mo.MessageID, clog).WithReceivedOn(date)
	for _, a := range attachments {
		msg = msg.WithAttachment(a)
	}
	return msg, nil
}

// Outbound payload shapes for Infobip's unified Messages API (`POST /messages-api/1/messages`). The Messages
// API is a single endpoint that dispatches to SMS, MMS, WhatsApp, Viber, Apple MFB, etc. via the `channel`
// discriminator on each message. This handler only sends SMS - the channel serves a shortcode that doesn't
// support MMS, so attachments (if any) get inlined into the SMS text via handlers.GetTextAndAttachments.
type ibOutboundPayload struct {
	Messages []ibOutboundMessage `json:"messages"`
}

type ibOutboundMessage struct {
	Channel      string            `json:"channel"` // "SMS" - Messages API discriminator
	Sender       string            `json:"sender"`
	Destinations []ibDestination   `json:"destinations"`
	Content      ibContent         `json:"content"`
	Options      *ibMessageOptions `json:"options,omitempty"`
	Webhooks     *ibWebhooks       `json:"webhooks,omitempty"`
}

type ibDestination struct {
	To        string `json:"to"`
	MessageID string `json:"messageId,omitempty"`
}

// ibContent wraps the message body. The Messages API body is a discriminated union (TEXT / IMAGE / VIDEO /
// DOCUMENT / MIXED / ...); this handler only emits TEXT because the shortcode is SMS-only.
type ibContent struct {
	Body ibTextBody `json:"body"`
}

type ibTextBody struct {
	Type string `json:"type"` // "TEXT"
	Text string `json:"text"`
}

// ibMessageOptions carries the per-message `options` block. Populated only for values that are actually set
// on the channel config - platform (application_id / entity_id) and transliteration - so an unset channel
// omits `options` entirely.
type ibMessageOptions struct {
	Platform        *ibPlatform `json:"platform,omitempty"`
	Transliteration string      `json:"transliteration,omitempty"`
}

// ibPlatform is Infobip's CPaaS-X application/entity tag. Empty means "don't send the field" - both children
// are omitempty and the parent is only attached when at least one is set.
type ibPlatform struct {
	ApplicationID string `json:"applicationId,omitempty"`
	EntityID      string `json:"entityId,omitempty"`
}

// ibWebhooks configures per-message delivery reporting. In the Messages API the `contentType` sits at this
// level (not nested under `delivery` like the old SMS v3 shape). intermediateReport=true keeps the same
// behavior as the previous SMS v3 handler.
type ibWebhooks struct {
	Delivery    ibDelivery `json:"delivery"`
	ContentType string     `json:"contentType,omitempty"`
}

type ibDelivery struct {
	URL                string `json:"url"`
	IntermediateReport bool   `json:"intermediateReport"`
}

func (h *handler) Send(ctx context.Context, msg courier.MsgOut, res *courier.SendResult, clog *courier.ChannelLog) error {
	apiKey := msg.Channel().StringConfigForKey(courier.ConfigAPIKey, "")
	username := msg.Channel().StringConfigForKey(courier.ConfigUsername, "")
	password := msg.Channel().StringConfigForKey(courier.ConfigPassword, "")

	if apiKey == "" && (username == "" || password == "") {
		return courier.ErrChannelConfig
	}

	baseURL := msg.Channel().StringConfigForKey(courier.ConfigBaseURL, "https://api.infobip.com")
	callbackDomain := msg.Channel().CallbackDomain(h.Server().Config().Domain)
	// Point the DLR webhook at the same /receive endpoint that inbound MO events land on - the Messages API
	// posts both event kinds in the same envelope, and the receiveEvents dispatcher classifies each result.
	statusURL := fmt.Sprintf("https://%s%s%s/receive", callbackDomain, "/c/ib/", msg.Channel().UUID())

	payload := ibOutboundPayload{
		Messages: []ibOutboundMessage{
			{
				Channel: "SMS",
				Sender:  msg.Channel().Address(),
				Destinations: []ibDestination{
					{
						To:        strings.TrimLeft(msg.URN().Path(), "+"),
						MessageID: msg.ID().String(),
					},
				},
				Content: ibContent{
					Body: ibTextBody{
						Type: "TEXT",
						Text: handlers.GetTextAndAttachments(msg),
					},
				},
				Options: messageOptions(msg.Channel()),
				Webhooks: &ibWebhooks{
					Delivery: ibDelivery{
						URL:                statusURL,
						IntermediateReport: true,
					},
					ContentType: "application/json",
				},
			},
		},
	}

	requestBody := &bytes.Buffer{}
	if err := json.NewEncoder(requestBody).Encode(payload); err != nil {
		return err
	}

	sendURL := fmt.Sprintf("%s/messages-api/1/messages", baseURL)
	req, err := http.NewRequest(http.MethodPost, sendURL, requestBody)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	if apiKey != "" {
		req.Header.Set("Authorization", "App "+apiKey)
	} else {
		req.SetBasicAuth(username, password)
	}

	resp, respBody, err := h.RequestHTTP(req, clog)
	if err != nil || resp.StatusCode/100 == 5 {
		return courier.ErrConnectionFailed
	} else if resp.StatusCode/100 != 2 {
		return courier.ErrResponseStatus
	}

	groupID, err := jsonparser.GetInt(respBody, "messages", "[0]", "status", "groupId")
	if err != nil || (groupID != 1 && groupID != 3) {
		return courier.ErrResponseUnparseable
	}

	externalID, err := jsonparser.GetString(respBody, "messages", "[0]", "messageId")
	if err != nil {
		clog.Error(courier.ErrorResponseValueMissing("messageId"))
	} else {
		res.AddExternalID(externalID)
	}

	return nil
}

// messageOptions builds the per-message `options` block from channel config, returning nil when nothing on
// the channel would populate it. Currently packs platform (application_id / entity_id) and transliteration;
// the same helper is the place to add validity period, delivery time window, or traffic type later.
func messageOptions(ch courier.Channel) *ibMessageOptions {
	appID := ch.StringConfigForKey(configApplicationID, "")
	entityID := ch.StringConfigForKey(configEntityID, "")
	transliteration := ch.StringConfigForKey(configTransliteration, "")

	opts := &ibMessageOptions{Transliteration: transliteration}
	if appID != "" || entityID != "" {
		opts.Platform = &ibPlatform{ApplicationID: appID, EntityID: entityID}
	}
	if opts.Platform == nil && opts.Transliteration == "" {
		return nil
	}
	return opts
}

func (h *handler) RedactValues(ch courier.Channel) []string {
	redacted := []string{}
	if apiKey := ch.StringConfigForKey(courier.ConfigAPIKey, ""); apiKey != "" {
		redacted = append(redacted, "App "+apiKey)
	}
	if username := ch.StringConfigForKey(courier.ConfigUsername, ""); username != "" {
		redacted = append(redacted, httpx.BasicAuth(username, ch.StringConfigForKey(courier.ConfigPassword, "")))
	}
	return redacted
}
