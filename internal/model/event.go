package model

import "encoding/json"

type CollectRequest struct {
	SiteID         string          `json:"site_id"`
	Environment    string          `json:"environment,omitempty"`
	TrackingKey    string          `json:"tracking_key,omitempty"`
	VisitorID      string          `json:"visitor_id"`
	SessionID      string          `json:"session_id"`
	UserID         string          `json:"user_id,omitempty"`
	UserProperties map[string]any  `json:"user_properties,omitempty"`
	Context        EventContext    `json:"context,omitempty"`
	Events         []IncomingEvent `json:"events"`
}

type IncomingEvent struct {
	ID              string           `json:"id,omitempty"`
	Name            string           `json:"name"`
	Timestamp       int64            `json:"timestamp"`
	Properties      map[string]any   `json:"properties,omitempty"`
	Items           []map[string]any `json:"items,omitempty"`
	Context         *EventContext    `json:"context,omitempty"`
	Debug           bool             `json:"debug,omitempty"`
	ContractVersion int              `json:"contract_version,omitempty"`
}

type EventContext struct {
	Page    PageContext    `json:"page,omitempty"`
	Device  DeviceContext  `json:"device,omitempty"`
	Traffic TrafficContext `json:"traffic,omitempty"`
}

type PageContext struct {
	URL      string `json:"url,omitempty"`
	Title    string `json:"title,omitempty"`
	Referrer string `json:"referrer,omitempty"`
}
type DeviceContext struct {
	Type     string `json:"type,omitempty"`
	Browser  string `json:"browser,omitempty"`
	OS       string `json:"os,omitempty"`
	Language string `json:"language,omitempty"`
	Screen   string `json:"screen,omitempty"`
}
type TrafficContext struct {
	Source   string `json:"source,omitempty"`
	Medium   string `json:"medium,omitempty"`
	Campaign string `json:"campaign,omitempty"`
	Term     string `json:"term,omitempty"`
	Content  string `json:"content,omitempty"`
}

type InboxPayload struct {
	Request        CollectRequest `json:"request"`
	ClientIP       string         `json:"client_ip"`
	Origin         string         `json:"origin"`
	UserAgent      string         `json:"user_agent"`
	ReceivedUnix   int64          `json:"received_unix"`
	PrivacyBlocked int            `json:"privacy_blocked,omitempty"`
}

func (p InboxPayload) JSON() ([]byte, error) { return json.Marshal(p) }
