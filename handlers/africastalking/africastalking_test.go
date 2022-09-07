package africastalking

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nyaruka/courier"
	. "github.com/nyaruka/courier/handlers"
	"github.com/nyaruka/courier/test"
)

var testChannels = []courier.Channel{
	test.NewMockChannel("8eb23e93-5ecb-45ba-b726-3b064e0c56ab", "AT", "2020", "US", nil),
}

const (
	receiveURL = "/c/at/8eb23e93-5ecb-45ba-b726-3b064e0c56ab/receive/"
	statusURL  = "/c/at/8eb23e93-5ecb-45ba-b726-3b064e0c56ab/status/"
)

var testCases = []ChannelHandleTestCase{
	{
		Label:              "Receive Valid",
		URL:                receiveURL,
		Data:               "linkId=03090445075804249226&text=Msg&to=21512&id=ec9adc86-51d5-4bc8-8eb0-d8ab0bb53dc3&date=2017-05-03T06%3A04%3A45Z&from=%2B254791541111",
		ExpectedRespStatus: 200,
		ExpectedRespBody:   "Message Accepted",
		ExpectedMsgText:    Sp("Msg"),
		ExpectedURN:        "tel:+254791541111",
		ExpectedExternalID: "ec9adc86-51d5-4bc8-8eb0-d8ab0bb53dc3",
		ExpectedDate:       time.Date(2017, 5, 3, 06, 04, 45, 0, time.UTC),
	},
	{
		Label:              "Receive Valid",
		URL:                receiveURL,
		Data:               "linkId=03090445075804249226&text=Msg&to=21512&id=ec9adc86-51d5-4bc8-8eb0-d8ab0bb53dc3&date=2017-05-03+06%3A04%3A45&from=%2B254791541111",
		ExpectedRespStatus: 200,
		ExpectedRespBody:   "Message Accepted",
		ExpectedMsgText:    Sp("Msg"),
		ExpectedURN:        "tel:+254791541111",
		ExpectedExternalID: "ec9adc86-51d5-4bc8-8eb0-d8ab0bb53dc3",
		ExpectedDate:       time.Date(2017, 5, 3, 06, 04, 45, 0, time.UTC),
	},
	{
		Label:              "Receive Empty",
		URL:                receiveURL,
		Data:               "empty",
		ExpectedRespStatus: 400,
		ExpectedRespBody:   "field 'id' required",
	},
	{
		Label:              "Receive Missing Text",
		URL:                receiveURL,
		Data:               "linkId=03090445075804249226&to=21512&id=ec9adc86-51d5-4bc8-8eb0-d8ab0bb53dc3&date=2017-05-03T06%3A04%3A45Z&from=%2B254791541111",
		ExpectedRespStatus: 400,
		ExpectedRespBody:   "field 'text' required",
	},
	{
		Label:              "Invalid URN",
		URL:                receiveURL,
		Data:               "linkId=03090445075804249226&text=Msg&to=21512&id=ec9adc86-51d5-4bc8-8eb0-d8ab0bb53dc3&date=2017-05-03T06%3A04%3A45Z&from=MTN",
		ExpectedRespStatus: 400,
		ExpectedRespBody:   "phone number supplied is not a number",
	},
	{
		Label:              "Invalid Date",
		URL:                receiveURL,
		Data:               "linkId=03090445075804249226&text=Msg&to=21512&id=ec9adc86-51d5-4bc8-8eb0-d8ab0bb53dc3&date=2017-05-03T06%3A04&from=%2B254791541111",
		ExpectedRespStatus: 400,
		ExpectedRespBody:   "invalid date format",
	},
	{
		Label:              "Status Invalid",
		URL:                statusURL,
		Data:               "id=ATXid_dda018a640edfcc5d2ce455de3e4a6e7&status=Borked",
		ExpectedRespStatus: 400,
		ExpectedRespBody:   "unknown status",
	},
	{
		Label:              "Status Missing",
		URL:                statusURL,
		Data:               "id=ATXid_dda018a640edfcc5d2ce455de3e4a6e7",
		ExpectedRespStatus: 400,
		ExpectedRespBody:   "field 'status' required",
	},
	{
		Label:              "Status Success",
		URL:                statusURL,
		Data:               "id=ATXid_dda018a640edfcc5d2ce455de3e4a6e7&status=Success",
		ExpectedRespStatus: 200,
		ExpectedRespBody:   `"status":"D"`,
		ExpectedMsgStatus:  courier.MsgDelivered,
	},
	{
		Label:              "Status Expired",
		URL:                statusURL,
		Data:               "id=ATXid_dda018a640edfcc5d2ce455de3e4a6e7&status=Expired",
		ExpectedRespStatus: 200,
		ExpectedRespBody:   `"status":"F"`,
		ExpectedMsgStatus:  courier.MsgFailed,
	},
}

func TestHandler(t *testing.T) {
	RunChannelTestCases(t, testChannels, newHandler(), testCases)
}

func BenchmarkHandler(b *testing.B) {
	RunChannelBenchmarks(b, testChannels, newHandler(), testCases)
}

// setSendURL takes care of setting the sendURL to call
func setSendURL(s *httptest.Server, h courier.ChannelHandler, c courier.Channel, m courier.Msg) {
	sendURL = s.URL
}

var defaultSendTestCases = []ChannelSendTestCase{
	{
		Label:              "Plain Send",
		MsgText:            "Simple Message ☺",
		MsgURN:             "tel:+250788383383",
		MockResponseBody:   `{ "SMSMessageData": {"Recipients": [{"status": "Success", "messageId": "1002"}] } }`,
		MockResponseStatus: 200,
		ExpectedHeaders:    map[string]string{"apikey": "KEY"},
		ExpectedPostParams: map[string]string{"message": "Simple Message ☺", "username": "Username", "to": "+250788383383", "from": "2020"},
		ExpectedMsgStatus:  "W",
		ExpectedExternalID: "1002",
		SendPrep:           setSendURL,
	},
	{
		Label:              "Send Attachment",
		MsgText:            "My pic!",
		MsgURN:             "tel:+250788383383",
		MsgAttachments:     []string{"image/jpeg:https://foo.bar/image.jpg"},
		MockResponseBody:   `{ "SMSMessageData": {"Recipients": [{"status": "Success", "messageId": "1002"}] } }`,
		MockResponseStatus: 200,
		ExpectedPostParams: map[string]string{"message": "My pic!\nhttps://foo.bar/image.jpg"},
		ExpectedMsgStatus:  "W",
		ExpectedExternalID: "1002",
		SendPrep:           setSendURL,
	},
	{
		Label:              "No External Id",
		MsgText:            "No External ID",
		MsgURN:             "tel:+250788383383",
		MockResponseBody:   `{ "SMSMessageData": {"Recipients": [{"status": "Failed" }] } }`,
		MockResponseStatus: 200,
		ExpectedPostParams: map[string]string{"message": `No External ID`},
		ExpectedMsgStatus:  "E",
		SendPrep:           setSendURL,
	},
	{
		Label:              "Error Sending",
		MsgText:            "Error Message",
		MsgURN:             "tel:+250788383383",
		MockResponseBody:   `{ "error": "failed" }`,
		MockResponseStatus: 401,
		ExpectedPostParams: map[string]string{"message": `Error Message`},
		ExpectedMsgStatus:  "E",
		SendPrep:           setSendURL,
	},
}

var sharedSendTestCases = []ChannelSendTestCase{
	{
		Label:              "Shared Send",
		MsgText:            "Simple Message ☺",
		MsgURN:             "tel:+250788383383",
		MockResponseBody:   `{ "SMSMessageData": {"Recipients": [{"status": "Success", "messageId": "1002"}] } }`,
		MockResponseStatus: 200,
		ExpectedHeaders:    map[string]string{"apikey": "KEY"},
		ExpectedPostParams: map[string]string{"message": "Simple Message ☺", "username": "Username", "to": "+250788383383", "from": ""},
		ExpectedMsgStatus:  "W",
		ExpectedExternalID: "1002",
		SendPrep:           setSendURL,
	},
}

func TestSending(t *testing.T) {
	var defaultChannel = test.NewMockChannel("8eb23e93-5ecb-45ba-b726-3b064e0c56ab", "AT", "2020", "US",
		map[string]interface{}{
			courier.ConfigUsername: "Username",
			courier.ConfigAPIKey:   "KEY",
		})
	var sharedChannel = test.NewMockChannel("8eb23e93-5ecb-45ba-b726-3b064e0c56ab", "AT", "2020", "US",
		map[string]interface{}{
			courier.ConfigUsername: "Username",
			courier.ConfigAPIKey:   "KEY",
			configIsShared:         true,
		})

	RunChannelSendTestCases(t, defaultChannel, newHandler(), defaultSendTestCases, nil)
	RunChannelSendTestCases(t, sharedChannel, newHandler(), sharedSendTestCases, nil)
}
