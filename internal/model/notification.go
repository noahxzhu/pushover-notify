package model

import "time"

type SendStatus string

const (
	StatusPending SendStatus = "Pending"
	StatusDone    SendStatus = "Done"
)

type Notification struct {
	ID             string     `json:"id"`
	Content        string     `json:"content"`
	ScheduledTime  time.Time  `json:"scheduled_time"`
	Status         SendStatus `json:"status"`
	SendsCount     int        `json:"sends_count"`
	LastPushTime   time.Time  `json:"last_push_time"`
	RepeatTimes    int        `json:"repeat_times"`
	RepeatInterval string     `json:"repeat_interval"`
}

type Settings struct {
	PushoverToken  string `json:"pushover_token"`
	PushoverUser   string `json:"pushover_user"`
	RepeatTimes    int    `json:"repeat_times"`
	RepeatInterval string `json:"repeat_interval"` // Duration string e.g. "30m"
	Password       string `json:"password"`        // Plain text
}

type AppSchema struct {
	Settings      Settings        `json:"settings"`
	Notifications []*Notification `json:"notifications"`
}
