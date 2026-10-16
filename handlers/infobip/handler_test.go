package infobip

import (
	"testing"
	"time"

	"github.com/nyaruka/courier"
	. "github.com/nyaruka/courier/handlers"
	"github.com/nyaruka/courier/test"
	"github.com/nyaruka/courier/utils/clogs"
	"github.com/nyaruka/gocommon/httpx"
	"github.com/nyaruka/gocommon/urns"
)

var testChannels = []courier.Channel{
	test.NewMockChannel("8eb23e93-5ecb-45ba-b726-3b064e0c56ab", "IB", "2020", "US", []string{urns.Phone.Prefix}, nil),
}

const (
	receiveURL = "/c/ib/8eb23e93-5ecb-45ba-b726-3b064e0c56ab/receive"
	statusURL  = "/c/ib/8eb23e93-5ecb-45ba-b726-3b064e0c56ab/delivered/"
)

var helloMsg = `{
	"results": [
		{
			"messageId": "817790313235066447",
			"from": "385916242493",
			"to": "385921004026",
			"receivedAt": "2016-10-06T09:28:39.220+0000",
			"message": [
				{
					"contentType": "text/plain",
					"value": "This is message text"
				},
				{
					"contentType": "image/jpeg",
					"url": "https://examplelink.com/123456"
				}
			],
			"price": {
				"pricePerMessage": 0,
				"currency": "EUR"
			}
		}
	],
	"messageCount": 1,
	"pendingMessageCount": 0
}`

var invalidURN = `{
	"results": [
		{
			"messageId": "817790313235066447",
			"from": "MTN",
			"to": "385921004026",
			"text": "QUIZ Correct answer is Paris",
			"cleanText": "Correct answer is Paris",
			"keyword": "QUIZ",
			"receivedAt": "2016-10-06T09:28:39.220+0000",
			"smsCount": 1,
			"price": {
				"pricePerMessage": 0,
				"currency": "EUR"
			},
			"callbackData": "callbackData"
		}
	],
	"messageCount": 1,
	"pendingMessageCount": 0
}`

var missingResults = `{
	"unexpected": [
	  {
		  "messageId": "817790313235066447",
		  "from": "385916242493",
		  "to": "385921004026",
		  "text": "QUIZ Correct answer is Paris",
		  "cleanText": "Correct answer is Paris",
		  "keyword": "QUIZ",
		  "receivedAt": "2016-10-06T09:28:39.220+0000",
		  "smsCount": 1,
		  "price": {
		   "pricePerMessage": 0,
		   "currency": "EUR"
		  },
		  "callbackData": "callbackData"
	  }
  ],
  "messageCount": 1,
  "pendingMessageCount": 0
}`

var missingText = `{
  	"results": [
		{
			"messageId": "817790313235066447",
			"from": "385916242493",
			"to": "385921004026",
			"text": "",
			"cleanText": "Correct answer is Paris",
			"keyword": "QUIZ",
			"receivedAt": "2016-10-06T09:28:39.220+0000",
			"smsCount": 1,
			"price": {
				"pricePerMessage": 0,
				"currency": "EUR"
			},
			"callbackData": "callbackData"
		}
	],
	"messageCount": 1,
	"pendingMessageCount": 0
}`

var invalidJSONStatus = "Invalid"

var statusMissingResultsKey = `{
	"deliveryReport": [
		{
			"messageId": "12345",
			"status": {
				"groupName": "DELIVERED"
			}
		}
	]
}`

var validStatusDelivered = `{
	"results": [
		{
			"messageId": "12345",
			"status": {
				"groupName": "DELIVERED"
			}
		}
	]
}`

var validStatusRejected = `{
	"results": [
		{
			"messageId": "12345",
			"status": {
				"groupName": "REJECTED"
			}
		}
	]
}`

var validStatusUndeliverable = `{
	"results": [
		{
			"messageId": "12345",
			"status": {
				"groupName": "UNDELIVERABLE"
			}
		}
	]
}`

var validStatusPending = `{
	"results": [
		{
			"messageId": "12345",
			"status": {
				"groupName": "PENDING"
			}
		},
		{
			"messageId": "12347",
			"status": {
				"groupName": "PENDING"
			}
		}		
	]
}`

var validStatusExpired = `{
	"results": [
		{
			"messageId": "12345",
			"status": {
				"groupName": "EXPIRED"
			}
		}
	]
}`

var invalidStatus = `{
	"results": [
		{
			"messageId": "12345",
			"status": {
				"groupName": "UNEXPECTED"
			}
		}
	]
}`

var testCases = []IncomingTestCase{
	{
		Label:                "Receive Valid MMS Message",
		URL:                  receiveURL,
		Data:                 helloMsg,
		ExpectedRespStatus:   200,
		ExpectedBodyContains: "Accepted",
		ExpectedMsgText:      Sp("This is message text"),
		ExpectedURN:          "tel:+385916242493",
		ExpectedExternalID:   "817790313235066447",
		ExpectedDate:         time.Date(2016, 10, 06, 9, 28, 39, 220000000, time.UTC),
		ExpectedAttachments:  []string{"image/jpeg:https://examplelink.com/123456"},
	},
	{
		Label: "Receive Valid SMS Message",
		URL:   receiveURL,
		Data: `{
			"results": [
				{
					"messageId": "817790313235066448",
					"from": "385916242494",
					"to": "385921004027",
					"text": "This is an SMS message",
					"receivedAt": "2016-10-06T09:28:40.000+0000",
					"smsCount": 1,
					"price": {
						"pricePerMessage": 0,
						"currency": "EUR"
					}
				}
			],
			"messageCount": 1,
			"pendingMessageCount": 0
		}`,
		ExpectedRespStatus:   200,
		ExpectedBodyContains: "Accepted",
		ExpectedMsgText:      Sp("This is an SMS message"),
		ExpectedURN:          "tel:+385916242494",
		ExpectedExternalID:   "817790313235066448",
		ExpectedDate:         time.Date(2016, 10, 06, 9, 28, 40, 0, time.UTC),
		ExpectedAttachments:  []string{},
	},
	{
		Label:                "Receive missing results key",
		URL:                  receiveURL,
		Data:                 missingResults,
		ExpectedRespStatus:   400,
		ExpectedBodyContains: "validation for 'Results' failed",
	},
	{
		Label:                "Receive missing text key",
		URL:                  receiveURL,
		Data:                 missingText,
		ExpectedRespStatus:   200,
		ExpectedBodyContains: "ignoring request, no message",
	},
	{
		Label:                "Invalid URN",
		URL:                  receiveURL,
		Data:                 invalidURN,
		ExpectedRespStatus:   400,
		ExpectedBodyContains: "not a possible number",
	},
	{
		Label:                "Status report invalid JSON",
		URL:                  statusURL,
		Data:                 invalidJSONStatus,
		ExpectedRespStatus:   400,
		ExpectedBodyContains: "unable to parse request JSON",
	},
	{
		Label:                "Status report missing results key",
		URL:                  statusURL,
		Data:                 statusMissingResultsKey,
		ExpectedRespStatus:   400,
		ExpectedBodyContains: "Field validation for 'Results' failed",
	},
	{
		Label:                "Status delivered",
		URL:                  statusURL,
		Data:                 validStatusDelivered,
		ExpectedRespStatus:   200,
		ExpectedBodyContains: `"status":"D"`,
		ExpectedStatuses:     []ExpectedStatus{{ExternalID: "12345", Status: courier.MsgStatusDelivered}},
	},
	{
		Label:                "Status rejected",
		URL:                  statusURL,
		Data:                 validStatusRejected,
		ExpectedRespStatus:   200,
		ExpectedBodyContains: `"status":"F"`,
		ExpectedStatuses:     []ExpectedStatus{{ExternalID: "12345", Status: courier.MsgStatusFailed}},
	},
	{
		Label:                "Status undeliverable",
		URL:                  statusURL,
		Data:                 validStatusUndeliverable,
		ExpectedRespStatus:   200,
		ExpectedBodyContains: `"status":"F"`,
		ExpectedStatuses:     []ExpectedStatus{{ExternalID: "12345", Status: courier.MsgStatusFailed}},
	},
	{
		Label:                "Status pending",
		URL:                  statusURL,
		Data:                 validStatusPending,
		ExpectedRespStatus:   200,
		ExpectedBodyContains: `"status":"S"`,
		ExpectedStatuses:     []ExpectedStatus{{ExternalID: "12345", Status: courier.MsgStatusSent}, {ExternalID: "12347", Status: courier.MsgStatusSent}},
	},
	{
		Label:                "Status expired",
		URL:                  statusURL,
		Data:                 validStatusExpired,
		ExpectedRespStatus:   200,
		ExpectedBodyContains: `"status":"S"`,
		ExpectedStatuses:     []ExpectedStatus{{ExternalID: "12345", Status: courier.MsgStatusSent}},
	},
	{
		Label:                "Status group name unexpected",
		URL:                  statusURL,
		Data:                 invalidStatus,
		ExpectedRespStatus:   400,
		ExpectedBodyContains: `unknown status 'UNEXPECTED'`,
	},
}

func TestIncoming(t *testing.T) {
	RunIncomingTestCases(t, testChannels, newHandler(), testCases)
}

var defaultSendTestCases = []OutgoingTestCase{
	{
		Label:   "Plain Send",
		MsgText: "Simple Message",
		MsgURN:  "tel:+250788383383",
		MockResponses: map[string][]*httpx.MockResponse{
			"https://api.infobip.com/sms/3/messages": {
				httpx.NewMockResponse(200, nil, []byte(`{"messages":[{"status":{"groupId": 1}, "messageId": "12345"}}`)),
			},
		},
		ExpectedRequests: []ExpectedRequest{{
			Headers: map[string]string{
				"Content-Type":  "application/json",
				"Accept":        "application/json",
				"Authorization": "Basic VXNlcm5hbWU6UGFzc3dvcmQ=",
			},
			Body: `{"messages":[{"from":"2020","destinations":[{"to":"250788383383","messageId":"10"}],"content":{"text":"Simple Message"},"webhooks":{"delivery":{"url":"https://localhost/c/ib/8eb23e93-5ecb-45ba-b726-3b064e0c56ab/delivered","intermediateReport":true,"contentType":"application/json"}}}]}`,
		}},
		ExpectedExtIDs: []string{"12345"},
	},
	{
		Label:   "Unicode Send",
		MsgText: "☺",
		MsgURN:  "tel:+250788383383",
		MockResponses: map[string][]*httpx.MockResponse{
			"https://api.infobip.com/sms/3/messages": {
				httpx.NewMockResponse(200, nil, []byte(`{"messages":[{"status":{"groupId": 1}}}`)),
			},
		},
		ExpectedRequests: []ExpectedRequest{{
			Headers: map[string]string{
				"Content-Type":  "application/json",
				"Accept":        "application/json",
				"Authorization": "Basic VXNlcm5hbWU6UGFzc3dvcmQ=",
			},
			Body: `{"messages":[{"from":"2020","destinations":[{"to":"250788383383","messageId":"10"}],"content":{"text":"☺"},"webhooks":{"delivery":{"url":"https://localhost/c/ib/8eb23e93-5ecb-45ba-b726-3b064e0c56ab/delivered","intermediateReport":true,"contentType":"application/json"}}}]}`,
		}},
		ExpectedLogErrors: []*clogs.LogError{courier.ErrorResponseValueMissing("messageId")},
	},
	{
		Label:          "Send MMS with Attachment",
		MsgText:        "Check out this image!",
		MsgURN:         "tel:+250788383383",
		MsgAttachments: []string{"image/jpeg:https://example.com/my_image.jpg"},
		MockResponses: map[string][]*httpx.MockResponse{
			"https://api.infobip.com/mms/2/messages": {
				httpx.NewMockResponse(200, nil, []byte(`{"messages":[{"status":{"groupId": 1}, "messageId": "mms-12345"}}`)),
			},
		},
		ExpectedRequests: []ExpectedRequest{{
			Headers: map[string]string{
				"Content-Type":  "application/json",
				"Accept":        "application/json",
				"Authorization": "Basic VXNlcm5hbWU6UGFzc3dvcmQ=",
			},
			Body: `{"messages":[{"sender":"2020","destinations":[{"to":"250788383383","messageId":"10"}],"content":{"title":"","messageSegments":[{"type":"TEXT","text":"Check out this image!"},{"type":"IMAGE","url":"https://example.com/my_image.jpg"}]},"webhooks":{"delivery":{"url":"https://localhost/c/ib/8eb23e93-5ecb-45ba-b726-3b064e0c56ab/delivered","intermediateReport":true,"contentType":"application/json"}}}]}`,
		}},
		ExpectedExtIDs: []string{"mms-12345"},
	},
	{
		Label:   "Error Sending",
		MsgText: "Error Message",
		MsgURN:  "tel:+250788383383",
		MockResponses: map[string][]*httpx.MockResponse{
			"https://api.infobip.com/sms/3/messages": {
				httpx.NewMockResponse(401, nil, []byte(`{ "error": "failed" }`)),
			},
		},
		ExpectedRequests: []ExpectedRequest{{
			Headers: map[string]string{
				"Content-Type":  "application/json",
				"Accept":        "application/json",
				"Authorization": "Basic VXNlcm5hbWU6UGFzc3dvcmQ=",
			},
			Body: `{"messages":[{"from":"2020","destinations":[{"to":"250788383383","messageId":"10"}],"content":{"text":"Error Message"},"webhooks":{"delivery":{"url":"https://localhost/c/ib/8eb23e93-5ecb-45ba-b726-3b064e0c56ab/delivered","intermediateReport":true,"contentType":"application/json"}}}]}`,
		}},
		ExpectedError: courier.ErrResponseStatus,
	},
	{
		Label:   "Error groupId",
		MsgText: "Simple Message",
		MsgURN:  "tel:+250788383383",
		MockResponses: map[string][]*httpx.MockResponse{
			"https://api.infobip.com/sms/3/messages": {
				httpx.NewMockResponse(200, nil, []byte(`{"messages":[{"status":{"groupId": 2}}}`)),
			},
		},
		ExpectedRequests: []ExpectedRequest{{
			Headers: map[string]string{
				"Content-Type":  "application/json",
				"Accept":        "application/json",
				"Authorization": "Basic VXNlcm5hbWU6UGFzc3dvcmQ=",
			},
			Body: `{"messages":[{"from":"2020","destinations":[{"to":"250788383383","messageId":"10"}],"content":{"text":"Simple Message"},"webhooks":{"delivery":{"url":"https://localhost/c/ib/8eb23e93-5ecb-45ba-b726-3b064e0c56ab/delivered","intermediateReport":true,"contentType":"application/json"}}}]}`,
		}},
		ExpectedError: courier.ErrResponseUnparseable,
	},
}

var transSendTestCases = []OutgoingTestCase{
	{
		Label:   "Plain Send",
		MsgText: "Simple Message",
		MsgURN:  "tel:+250788383383",
		MockResponses: map[string][]*httpx.MockResponse{
			"https://api.infobip.com/sms/3/messages": {
				httpx.NewMockResponse(200, nil, []byte(`{"messages":[{"status":{"groupId": 1}, "messageId": "12345"}}`)),
			},
		},
		ExpectedRequests: []ExpectedRequest{{
			Headers: map[string]string{
				"Content-Type":  "application/json",
				"Accept":        "application/json",
				"Authorization": "Basic VXNlcm5hbWU6UGFzc3dvcmQ=",
			},
			Body: `{"messages":[{"from":"2020","destinations":[{"to":"250788383383","messageId":"10"}],"content":{"text":"Simple Message","transliteration":"COLOMBIAN"},"webhooks":{"delivery":{"url":"https://localhost/c/ib/8eb23e93-5ecb-45ba-b726-3b064e0c56ab/delivered","intermediateReport":true,"contentType":"application/json"}}}]}`,
		}},
		ExpectedExtIDs: []string{"12345"},
	},
}

var apiKeySendTestCases = []OutgoingTestCase{
	{
		Label:   "API Key Send",
		MsgText: "API Key Message",
		MsgURN:  "tel:+250788383383",
		MockResponses: map[string][]*httpx.MockResponse{
			"https://api.infobip.com/sms/3/messages": {
				httpx.NewMockResponse(200, nil, []byte(`{"messages":[{"status":{"groupId": 1}, "messageId": "67890"}}`)),
			},
		},
		ExpectedRequests: []ExpectedRequest{{
			Headers: map[string]string{
				"Content-Type":  "application/json",
				"Accept":        "application/json",
				"Authorization": "App test-api-key",
			},
			Body: `{"messages":[{"from":"2020","destinations":[{"to":"250788383383","messageId":"10"}],"content":{"text":"API Key Message"},"webhooks":{"delivery":{"url":"https://localhost/c/ib/8eb23e93-5ecb-45ba-b726-3b064e0c56ab/delivered","intermediateReport":true,"contentType":"application/json"}}}]}`,
		}},
		ExpectedExtIDs: []string{"67890"},
	},
}

func TestOutgoing(t *testing.T) {
	var defaultChannel = test.NewMockChannel("8eb23e93-5ecb-45ba-b726-3b064e0c56ab", "IB", "2020", "US",
		[]string{urns.Phone.Prefix},
		map[string]any{
			courier.ConfigPassword: "Password",
			courier.ConfigUsername: "Username",
		})

	RunOutgoingTestCases(t, defaultChannel, newHandler(), defaultSendTestCases, []string{httpx.BasicAuth("Username", "Password")}, nil)

	var transChannel = test.NewMockChannel("8eb23e93-5ecb-45ba-b726-3b064e0c56ab", "IB", "2020", "US",
		[]string{urns.Phone.Prefix},
		map[string]any{
			courier.ConfigPassword: "Password",
			courier.ConfigUsername: "Username",
			configTransliteration:  "COLOMBIAN",
		})

	RunOutgoingTestCases(t, transChannel, newHandler(), transSendTestCases, []string{httpx.BasicAuth("Username", "Password")}, nil)

	var apiKeyChannel = test.NewMockChannel("8eb23e93-5ecb-45ba-b726-3b064e0c56ab", "IB", "2020", "US",
		[]string{urns.Phone.Prefix},
		map[string]any{
			courier.ConfigAPIKey: "test-api-key",
		})

	RunOutgoingTestCases(t, apiKeyChannel, newHandler(), apiKeySendTestCases, []string{"App test-api-key"}, nil)
}

var messagesAPITestCases = []IncomingTestCase{
	{
		Label:              "Receive MMS with subject",
		URL:                receiveURL,
		Data:               `{"results":[{"event":"MO","channel":"MMS","sender":"385916242493","destination":"2020","messageId":"mo-1","receivedAt":"2016-10-06T09:28:39.220+0000","content":[{"type":"SUBJECT","text":"Holiday"},{"type":"TEXT","text":"Look at this"}]}]}`,
		ExpectedRespStatus: 200,
		ExpectedMsgText:    Sp("Holiday\nLook at this"),
		ExpectedURN:        "tel:+385916242493",
		ExpectedExternalID: "mo-1",
		ExpectedDate:       time.Date(2016, 10, 6, 9, 28, 39, 220000000, time.FixedZone("", 0)),
	},
	{
		Label:              "Delivery report with error",
		URL:                receiveURL,
		Data:               `{"results":[{"event":"DELIVERY","channel":"SMS","messageId":"12345","status":{"groupId":5,"groupName":"UNDELIVERABLE","name":"UNDELIVERABLE_NOT_DELIVERED"},"error":{"id":27,"name":"EC_ABSENT_SUBSCRIBER","description":"Absent Subscriber","permanent":false}}]}`,
		ExpectedRespStatus: 200,
		ExpectedStatuses:   []ExpectedStatus{{ExternalID: "12345", Status: courier.MsgStatusFailed}},
		ExpectedErrors:     []*clogs.LogError{courier.ErrorExternal("EC_ABSENT_SUBSCRIBER", "Absent Subscriber")},
	},
	{
		Label:              "Delivery report without error",
		URL:                receiveURL,
		Data:               `{"results":[{"event":"DELIVERY","channel":"SMS","messageId":"12345","status":{"groupId":3,"groupName":"DELIVERED","name":"DELIVERED_TO_HANDSET"},"error":{"id":0,"name":"NO_ERROR","description":"No Error","permanent":false}}]}`,
		ExpectedRespStatus: 200,
		ExpectedStatuses:   []ExpectedStatus{{ExternalID: "12345", Status: courier.MsgStatusDelivered}},
	},
}

func TestMessagesAPIIncoming(t *testing.T) {
	RunIncomingTestCases(t, testChannels, newHandler(), messagesAPITestCases)
}

var messagesAPISendTestCases = []OutgoingTestCase{
	{
		Label:   "Rejected in response",
		MsgText: "Simple Message",
		MsgURN:  "tel:+250788383383",
		MockResponses: map[string][]*httpx.MockResponse{
			"https://api.infobip.com/messages-api/1/messages": {
				httpx.NewMockResponse(200, nil, []byte(`{"messages":[{"messageId":"12345","status":{"groupId":5,"groupName":"REJECTED","id":52,"name":"REJECTED_DESTINATION","description":"Invalid destination address"}}]}`)),
			},
		},
		ExpectedRequests: []ExpectedRequest{{
			Headers: map[string]string{
				"Content-Type":  "application/json",
				"Accept":        "application/json",
				"Authorization": "Basic VXNlcm5hbWU6UGFzc3dvcmQ=",
			},
		}},
		ExpectedError:     courier.ErrResponseUnparseable,
		ExpectedLogErrors: []*clogs.LogError{courier.ErrorExternal("REJECTED_DESTINATION", "Invalid destination address")},
	},
}

func TestMessagesAPIOutgoing(t *testing.T) {
	ch := test.NewMockChannel("8eb23e93-5ecb-45ba-b726-3b064e0c56ab", "IB", "2020", "US", []string{urns.Phone.Prefix},
		map[string]any{courier.ConfigPassword: "Password", courier.ConfigUsername: "Username"})
	RunOutgoingTestCases(t, ch, newHandler(), messagesAPISendTestCases, []string{httpx.BasicAuth("Username", "Password")}, nil)
}
